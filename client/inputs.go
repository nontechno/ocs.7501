package main

// Inputs that Claude Code (2.1.302) writes to its terminal besides OSC 7501,
// plus the checks that tell whether the terminal answered the queries.
// Everything here is independent of the UI so it can be unit tested.
//
// The byte strings were captured from Claude Code itself; claude-server.md
// describes where each one comes from and what a terminal should do with it.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	esc = "\x1b"
	bel = "\x07"
)

type fieldKind int

const (
	fInput fieldKind = iota // free text
	fDrop                   // choose one of opts
)

// field is one entry of the payload form of a generic command.
type field struct {
	label string
	kind  fieldKind
	def   string   // default value (for fDrop: one of opts)
	opts  []string // fDrop only
}

// expect is one reply a terminal should send back for a query.
type expect struct {
	name     string         // e.g. "XTVERSION reply"
	shape    string         // human-readable byte shape
	re       *regexp.Regexp // matched against the received bytes
	optional bool           // a terminal without the feature sends nothing
}

// genCmd describes a command that is a list of fixed or lightly templated
// byte strings.
type genCmd struct {
	usesTerm bool       // shows the terminator dropdown
	defTerm  Terminator // terminator pre-selected for this command
	fields   []field
	// build returns the sequences to write, one per element, or validation
	// errors. Sequences are sent back to back in one write.
	build func(v map[string]string, t Terminator) ([]string, []string)
	// expects returns the replies to look for after sending (nil = none).
	expects func(v map[string]string) []expect
	// redraw asks the UI to repaint after sending: the sequences change
	// terminal state (screen, modes) behind the UI's back.
	redraw bool
}

// ---- helpers --------------------------------------------------------------

func hasControl(s string) (rune, bool) {
	for _, r := range s {
		if unicode.IsControl(r) {
			return r, true
		}
	}
	return 0, false
}

// text validates a single-line free-text field.
func text(name, s string, errs *[]string) string {
	if !utf8.ValidString(s) {
		*errs = append(*errs, name+": not valid UTF-8")
	}
	if r, bad := hasControl(s); bad {
		*errs = append(*errs, fmt.Sprintf("%s: contains control character U+%04X (single line only)", name, r))
	}
	return s
}

// leadInt returns the integer at the start of a dropdown label such as
// "2004  bracketed paste".
func leadInt(s string) int {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(f[0])
	return n
}

var idRe = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,32}$`)

// modeOpts are the DEC private modes Claude Code was seen to set, reset or query.
var modeOpts = []string{
	"1049  alternate screen",
	"25  cursor visible",
	"2004  bracketed paste",
	"2031  colour-scheme reports",
	"1004  focus events",
	"1000  mouse buttons",
	"1002  mouse drag",
	"1003  mouse any motion",
	"1006  mouse SGR encoding",
	"1016  mouse pixel coordinates",
	"2026  synchronized output",
}

// The 12 sequences Claude Code writes after its first frame, in order.
// The OSC 7501 query ends in BEL there; the graphics probe ends in ST.
var startupBurst = []string{
	esc + "[>0q",
	esc + "[?u",
	esc + "]7501;?" + bel,
	esc + "[c",
	esc + "[<u",
	esc + "[>5u",
	esc + "[>4;2m",
	esc + "[?2026$p",
	esc + "_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA" + esc + "\\",
	esc + "[16t",
	esc + "[?1016$p",
	esc + "[c",
}

// Mode switches and screen setup written before the first frame.
var startupModes = []string{
	esc + "7", esc + "[r", esc + "8",
	esc + "[?25h", esc + "[?2004h", esc + "[?2031h", esc + "[?1004h",
	esc + "[?1049h", esc + "[2J", esc + "[H",
	esc + "[?1000h", esc + "[?1002h", esc + "[?1003h", esc + "[?1006h",
	esc + "[?25l",
}

// What Claude Code writes on the second Ctrl-C, in the order observed.
var exitCleanup = []string{
	esc + "[?1006l", esc + "[?1003l", esc + "[?1002l", esc + "[?1000l", esc + "[?25h",
	esc + "[?1004l", esc + "[<u", esc + "[?1049l", esc + "[>4m", esc + "[>4m", esc + "[<u",
	esc + "[?2031l", esc + "[?2004l", esc + "(B\x0f", esc + "[?1016l",
	esc + "[?1006l", esc + "[?1003l", esc + "[?1002l", esc + "[?1000l", esc + "[?25h",
	esc + "7", esc + "[r", esc + "8", esc + "]7501;state=clear" + bel,
	esc + "[?25h", esc + "]7501;state=clear" + bel,
	esc + "(B\x0f", esc + "[?1016l", esc + "[?1006l", esc + "[?1003l", esc + "[?1002l", esc + "[?1000l",
	esc + "[>4m", esc + "[?1004l", esc + "[?2031l", esc + "[?2004l", esc + "[<u", esc + "[?25h",
	esc + "7", esc + "[r", esc + "8", esc + "]7501;state=clear" + bel, esc + "]0;" + bel, esc + "[?25h",
}

// ---- reply patterns ---------------------------------------------------------

var (
	reXTVersion = regexp.MustCompile("\x1bP>\\|[^\x1b]*\x1b\\\\")
	reKittyKbd  = regexp.MustCompile("\x1b\\[\\?\\d+u")
	reDA1       = regexp.MustCompile("\x1b\\[\\?[\\d;]*c")
	reCellSize  = regexp.MustCompile("\x1b\\[6;\\d+;\\d+t")
	reGraphics  = regexp.MustCompile("\x1b_Gi=31;[^\x1b]*\x1b\\\\")
	re7501      = regexp.MustCompile("\x1b\\]7501;\\?:states=[^:\x07\x1b]*:kinds=[^\x07\x1b]*(\x1b\\\\|\x07)")
)

func reDECRQM(mode int) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf("\x1b\\[\\?%d;[0-4]\\$y", mode))
}

var (
	exXTVersion = expect{"XTVERSION reply", "ESC P > | <name and version> ESC \\", reXTVersion, false}
	exKittyKbd  = expect{"kitty keyboard flags reply", "ESC [ ? <flags> u", reKittyKbd, false}
	exDA1       = expect{"primary device attributes (DA1) reply", "ESC [ ? <numbers separated by ;> c", reDA1, false}
	exCellSize  = expect{"cell size reply", "ESC [ 6 ; <height px> ; <width px> t", reCellSize, false}
	exGraphics  = expect{"kitty graphics reply", "ESC _ G i=31 ; OK ESC \\   (only if graphics are supported)", reGraphics, true}
	ex7501      = expect{"OSC 7501 feature-detection reply", "ESC ] 7501 ; ? : states=<list> : kinds=<list> ESC \\  (or ending in BEL)", re7501, false}
)

func exDECRQM(mode int) expect {
	return expect{
		fmt.Sprintf("DECRQM reply for mode %d", mode),
		fmt.Sprintf("ESC [ ? %d ; <0-4> $ y", mode),
		reDECRQM(mode), false,
	}
}

// ---- the commands -------------------------------------------------------------

func one(s string) func(map[string]string, Terminator) ([]string, []string) {
	return func(map[string]string, Terminator) ([]string, []string) { return []string{s}, nil }
}

func list(ss []string) func(map[string]string, Terminator) ([]string, []string) {
	return func(map[string]string, Terminator) ([]string, []string) { return ss, nil }
}

func expectOne(e expect) func(map[string]string) []expect {
	return func(map[string]string) []expect { return []expect{e} }
}

func actionDrop(a, b string) field {
	return field{label: "action", kind: fDrop, def: a, opts: []string{a, b}}
}

var genCommands = []command{
	{name: "title (OSC 0)", desc: "Set the tab/window title (Claude Code: BEL-terminated)", kind: kindGeneric,
		gen: &genCmd{
			usesTerm: true, defTerm: TermBEL,
			fields: []field{{label: "title", def: "✳ Claude Code"}},
			build: func(v map[string]string, t Terminator) ([]string, []string) {
				var errs []string
				s := text("title", v["title"], &errs)
				return []string{esc + "]0;" + s + t.Bytes()}, errs
			},
		}},
	{name: "bell (BEL)", desc: "Bare 0x07: ring the bell / flag the tab", kind: kindGeneric,
		gen: &genCmd{build: one(bel)}},
	{name: "notify · OSC 9", desc: "Desktop notification, one text (iterm2 channel)", kind: kindGeneric,
		gen: &genCmd{
			usesTerm: true, defTerm: TermBEL,
			fields: []field{{label: "text", def: "Claude needs your permission"}},
			build: func(v map[string]string, t Terminator) ([]string, []string) {
				var errs []string
				s := text("text", v["text"], &errs)
				return []string{esc + "]9;" + s + t.Bytes()}, errs
			},
		}},
	{name: "notify · OSC 99", desc: "Kitty notification: title, body, focus (3 sequences)", kind: kindGeneric,
		gen: &genCmd{
			usesTerm: true, defTerm: TermBEL,
			fields: []field{
				{label: "id", def: "2325"},
				{label: "title", def: "Claude Code"},
				{label: "body", def: "Claude needs your permission"},
			},
			build: func(v map[string]string, t Terminator) ([]string, []string) {
				var errs []string
				id := v["id"]
				if !idRe.MatchString(id) {
					errs = append(errs, "id: must match [A-Za-z0-9_.+-]{1,32}")
				}
				title := text("title", v["title"], &errs)
				body := text("body", v["body"], &errs)
				tb := t.Bytes()
				return []string{
					esc + "]99;i=" + id + ":d=0:p=title;" + title + tb,
					esc + "]99;i=" + id + ":p=body;" + body + tb,
					esc + "]99;i=" + id + ":d=1:a=focus;" + tb,
				}, errs
			},
		}},
	{name: "notify · OSC 777", desc: "Notification with title and body (ghostty channel)", kind: kindGeneric,
		gen: &genCmd{
			usesTerm: true, defTerm: TermBEL,
			fields: []field{
				{label: "title", def: "Claude Code"},
				{label: "body", def: "Claude needs your permission"},
			},
			build: func(v map[string]string, t Terminator) ([]string, []string) {
				var errs []string
				title := text("title", v["title"], &errs)
				body := text("body", v["body"], &errs)
				if strings.Contains(title, ";") {
					errs = append(errs, "title: ';' separates the fields and cannot appear in the title")
				}
				return []string{esc + "]777;notify;" + title + ";" + body + t.Bytes()}, errs
			},
		}},
	{name: "query · XTVERSION", desc: "Ask for the terminal's name and version", kind: kindGeneric,
		gen: &genCmd{build: one(esc + "[>0q"), expects: expectOne(exXTVersion)}},
	{name: "query · kitty keyboard", desc: "Ask which kitty keyboard flags are set", kind: kindGeneric,
		gen: &genCmd{build: one(esc + "[?u"), expects: expectOne(exKittyKbd)}},
	{name: "query · device attributes", desc: "DA1: every terminal should answer", kind: kindGeneric,
		gen: &genCmd{build: one(esc + "[c"), expects: expectOne(exDA1)}},
	{name: "query · mode (DECRQM)", desc: "Ask whether a DEC private mode is set", kind: kindGeneric,
		gen: &genCmd{
			fields: []field{{label: "mode", kind: fDrop, def: "2026  synchronized output", opts: modeOpts}},
			build: func(v map[string]string, _ Terminator) ([]string, []string) {
				return []string{fmt.Sprintf("%s[?%d$p", esc, leadInt(v["mode"]))}, nil
			},
			expects: func(v map[string]string) []expect { return []expect{exDECRQM(leadInt(v["mode"]))} },
		}},
	{name: "query · cell size", desc: "Ask for the cell size in pixels (CSI 16 t)", kind: kindGeneric,
		gen: &genCmd{build: one(esc + "[16t"), expects: expectOne(exCellSize)}},
	{name: "query · kitty graphics", desc: "1x1 pixel graphics probe (reply only if supported)", kind: kindGeneric,
		gen: &genCmd{build: one(startupBurst[8]), expects: expectOne(exGraphics)}},
	{name: "query · startup burst", desc: "All 12 queries Claude Code sends at startup, in order", kind: kindGeneric,
		gen: &genCmd{
			build: list(startupBurst),
			expects: func(map[string]string) []expect {
				return []expect{exXTVersion, exKittyKbd, ex7501, exDA1, exDECRQM(2026), exGraphics,
					exCellSize, exDECRQM(1016), exDA1}
			},
		}},
	{name: "mode · set / reset", desc: "Switch one DEC private mode on or off", kind: kindGeneric,
		gen: &genCmd{
			redraw: true,
			fields: []field{
				{label: "mode", kind: fDrop, def: "2004  bracketed paste", opts: modeOpts},
				actionDrop("set", "reset"),
			},
			build: func(v map[string]string, _ Terminator) ([]string, []string) {
				final := "h"
				if v["action"] == "reset" {
					final = "l"
				}
				return []string{fmt.Sprintf("%s[?%d%s", esc, leadInt(v["mode"]), final)}, nil
			},
		}},
	{name: "modes · Claude Code startup", desc: "Mode setup before the first frame (changes screen; run exit after)", kind: kindGeneric,
		gen: &genCmd{build: list(startupModes), redraw: true}},
	{name: "modes · Claude Code exit", desc: "Cleanup Claude Code writes on exit (also clears 7501 state)", kind: kindGeneric,
		gen: &genCmd{build: list(exitCleanup), redraw: true}},
	{name: "keyboard · kitty push/pop", desc: "Push or pop the kitty keyboard flag stack", kind: kindGeneric,
		gen: &genCmd{
			fields: []field{actionDrop("push", "pop"), {label: "flags", def: "5"}},
			build: func(v map[string]string, _ Terminator) ([]string, []string) {
				if v["action"] == "pop" {
					return []string{esc + "[<u"}, nil
				}
				n, err := strconv.Atoi(strings.TrimSpace(v["flags"]))
				if err != nil || n < 0 || n > 31 {
					return nil, []string{"flags: integer 0-31 (1 disambiguate, 2 event types, 4 alternate keys, 8 all keys, 16 text)"}
				}
				return []string{fmt.Sprintf("%s[>%du", esc, n)}, nil
			},
		}},
	{name: "keyboard · modifyOtherKeys", desc: "xterm modifyOtherKeys on (level) or off", kind: kindGeneric,
		gen: &genCmd{
			fields: []field{actionDrop("set", "reset"), {label: "level", def: "2"}},
			build: func(v map[string]string, _ Terminator) ([]string, []string) {
				if v["action"] == "reset" {
					return []string{esc + "[>4m"}, nil
				}
				n, err := strconv.Atoi(strings.TrimSpace(v["level"]))
				if err != nil || n < 0 || n > 2 {
					return nil, []string{"level: 0, 1 or 2"}
				}
				return []string{fmt.Sprintf("%s[>4;%dm", esc, n)}, nil
			},
		}},
}
