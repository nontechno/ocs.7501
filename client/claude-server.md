# claude-server.md — implementing the terminal side of Claude Code's inputs

Audience: a coding agent that implements, inside a terminal emulator, the **receiving** side of every
input that Claude Code (CLI, version 2.1.302) writes besides OSC 7501 (the Program Status Protocol,
specified at https://www.superlogical.com/rex/docs/build/program-status). The sender-side test tool is
the `ocs7501` client in this directory: every item below names the client menu entry that sends it.

Everything marked **observed** is bytes Claude Code actually wrote to a 120x40 pseudo-terminal in a
scripted session (startup, one permission prompt, completion, exit). Everything marked **spec** takes its
byte format from a published specification (listed at the end). Nothing here is guessed; where the byte
format of something was not available it says so under "Not specified here".

Notation: `ESC` = 0x1B, `BEL` = 0x07, `ST` = `ESC \` (0x1B 0x5C), `SI` = 0x0F, `CSI` = `ESC [`,
`OSC` = `ESC ]`, `APC` = `ESC _`, `DCS` = `ESC P`. Spaces inside a sequence below are for reading only;
there are no spaces on the wire unless a text field contains one.

## 1. What to build

1. A byte-stream parser for the program's output that survives arbitrary chunking (section 2).
2. A responder that writes replies to the program's **stdin** (the pty master), in the order the queries
   arrived (section 3).
3. State tracking for the DEC modes, the kitty keyboard flag stack and modifyOtherKeys, and the matching
   input encoders (section 5).
4. UI actions: tab title, notifications, bell, status reports (sections 4, 6, 7).
5. Idempotent handling of the exit cleanup (section 8).

## 2. Parsing requirements

* OSC strings end with **either** `BEL` or `ST`. Claude Code uses `BEL` for every OSC it writes
  (observed: OSC 0, 7501, 9, 99, 777). `BEL` ends only an OSC; a `BEL` outside any string is the bell
  (section 7). APC and DCS end only with `ST`.
* A sequence can be split across reads at any byte. Feed bytes one at a time in tests.
* CSI = `ESC [`, optional private marker `< = > ?`, parameter bytes `0x30-0x3F`, intermediate bytes
  `0x20-0x2F`, final byte `0x40-0x7E`. Examples seen: `ESC [ > 0 q`, `ESC [ ? 2026 $ p`, `ESC [ > 4 ; 2 m`.
* Two-byte escapes seen: `ESC 7`, `ESC 8`, `ESC ( B`.
* Unknown sequences must be consumed silently. They must not print, and must not be answered.
* Reference implementation: section 9 (`parser.go`).

## 3. Startup: queries and the exact replies

After drawing its first frame Claude Code writes these 12 sequences back to back (**observed**; client
entry `query · startup burst`, which sends exactly this byte string):

```
ESC [ > 0 q
ESC [ ? u
ESC ] 7501 ; ? BEL
ESC [ c
ESC [ < u
ESC [ > 5 u
ESC [ > 4 ; 2 m
ESC [ ? 2026 $ p
ESC _ G i=31,s=1,v=1,a=q,t=d,f=24 ; AAAA ESC \
ESC [ 1 6 t
ESC [ ? 1016 $ p
ESC [ c
```

The same burst as hex (98 bytes):

```
1b 5b 3e 30 71 1b 5b 3f 75 1b 5d 37 35 30 31 3b 3f 07 1b 5b 63 1b 5b 3c 75 1b 5b 3e 35 75 1b 5b 3e 34 3b 32 6d 1b 5b 3f 32 30 32 36 24 70 1b 5f 47 69 3d 33 31 2c 73 3d 31 2c 76 3d 31 2c 61 3d 71 2c 74 3d 64 2c 66 3d 32 34 3b 41 41 41 41 1b 5c 1b 5b 31 36 74 1b 5b 3f 31 30 31 36 24 70 1b 5b 63
```

To make a fixture file, run `ocs7501 -out burst.bin`, choose `query · startup burst`, press Enter, and replay
`burst.bin` in tests.

Replies. Write each reply to the program's stdin as soon as its query is parsed, in query order:

| # | Query (observed) | Client entry | Reply bytes | Source of reply format |
|---|---|---|---|---|
| 1 | `ESC [ > 0 q` (XTVERSION) | `query · XTVERSION` | `ESC P > \| <name> <version> ESC \` e.g. `ESC P > \| myterm 1.0 ESC \` | spec (xterm) |
| 2 | `ESC [ ? u` | `query · kitty keyboard` | `ESC [ ? <flags> u`, flags = current kitty flags of the active screen, `0` if none, e.g. `ESC [ ? 0 u` | spec (kitty keyboard) |
| 3 | `ESC ] 7501 ; ? BEL` | `? feature detection` | `ESC ] 7501 ; ? : states=idle,working,done,blocked,error,clear : kinds=permission,question,auth ESC \` — written **without** the spaces around `:` and `=` | spec (OSC 7501) |
| 4 | `ESC [ c` (DA1) | `query · device attributes` | `ESC [ ? 6 2 c` (any valid DA1 reply: `ESC [ ? <numbers> c`) | spec (xterm) |
| 5 | `ESC [ < u` | `keyboard · kitty push/pop` | none (pops one entry of the kitty flag stack) | spec (kitty keyboard) |
| 6 | `ESC [ > 5 u` | same | none (push flags 5) | spec (kitty keyboard) |
| 7 | `ESC [ > 4 ; 2 m` | `keyboard · modifyOtherKeys` | none (modifyOtherKeys level 2) | spec (xterm) |
| 8 | `ESC [ ? 2026 $ p` (DECRQM) | `query · mode (DECRQM)` | `ESC [ ? 2026 ; <Ps> $ y`, Ps: 0 not recognised, 1 set, 2 reset, 3 permanently set, 4 permanently reset | spec (xterm) |
| 9 | `ESC _ G i=31,s=1,v=1,a=q,t=d,f=24 ; AAAA ESC \` | `query · kitty graphics` | if graphics are supported: `ESC _ G i=31 ; OK ESC \`; otherwise **no reply** | spec (kitty graphics) |
| 10 | `ESC [ 1 6 t` | `query · cell size` | `ESC [ 6 ; <cell height px> ; <cell width px> t`, e.g. `ESC [ 6 ; 18 ; 9 t` | spec (xterm) |
| 11 | `ESC [ ? 1016 $ p` (DECRQM) | `query · mode (DECRQM)` | `ESC [ ? 1016 ; <Ps> $ y` | spec (xterm) |
| 12 | `ESC [ c` (DA1) | `query · device attributes` | `ESC [ ? 6 2 c` | spec (xterm) |

Rules that matter:

* **Order.** Answer the OSC 7501 query *before* the DA1 reply that follows it. In the capture runs, when DA1
  was answered first Claude Code sent **no** OSC 7501 reports at all; when the 7501 reply came first the
  reports appeared (observed). Never reorder replies.
* DA1 is sent twice (rows 4 and 12). Answer both.
* A feature you do not implement: send **no reply** for its query (rows 8, 9, 11 especially), but always
  answer DA1. Do not send `Ps = 0` replies unless you really want to say "mode not recognised".
* What Claude Code does with replies 1, 2, 8, 9, 10, 11 was not tested; implement them as specified and
  do not depend on any particular reaction.

Expected reply bytes for a terminal with 9x18 pixel cells and no optional features (what the client's
reply check looks for after `query · startup burst`, in this order):

```
ESC P > | myterm 1.0 ESC \
ESC [ ? 0 u
ESC ] 7501 ; ? : states=idle,working,done,blocked,error,clear : kinds=permission,question,auth ESC \
ESC [ ? 6 2 c
ESC [ ? 2026 ; 0 $ y
ESC [ 6 ; 18 ; 9 t
ESC [ ? 1016 ; 0 $ y
ESC [ ? 6 2 c
```

## 4. OSC 7501 reports (observed in the session)

Terminator is `BEL`. Keys seen: `state`, `app`, `kind`, `msg` only (no `id`, `progress`, `title`).

| When | Bytes |
|---|---|
| after the startup queries | `ESC ] 7501 ; state=idle:app=claude-code BEL` |
| model running | `ESC ] 7501 ; state=working:app=claude-code BEL` |
| permission prompt | `ESC ] 7501 ; state=blocked:app=claude-code:kind=permission:msg=YXBwcm92ZSBCYXNoOiB0b3VjaCAvdG1wL2NhcHdvcmsvbW9ja2ZpbGUudHh0 BEL` (msg decodes to `approve Bash: touch /tmp/capwork/mockfile.txt`) |
| turn finished | `ESC ] 7501 ; state=done:app=claude-code:msg=TW9jayByZXBseTogZG9uZS4= BEL` (msg decodes to `Mock reply: done.`) |
| exit | `ESC ] 7501 ; state=clear BEL` (written three times) |

`msg` and `title` are standard base64 of UTF-8. Follow the OSC 7501 specification for records (`id`),
limits and rendering; the client entries `idle`, `working`, `done`, `blocked`, `error`, `clear` send the
full set of keys for testing.

## 5. Modes, keyboard and input encodings

Claude Code sets these before its first frame (**observed**, in this order; client entry
`modes · Claude Code startup`):

```
ESC 7  ESC [ r  ESC 8
ESC [ ? 25 h  ESC [ ? 2004 h  ESC [ ? 2031 h  ESC [ ? 1004 h
ESC ] 0 ; ✳ Claude Code BEL
ESC [ ? 1049 h  ESC [ 2 J  ESC [ H
ESC [ ? 1000 h  ESC [ ? 1002 h  ESC [ ? 1003 h  ESC [ ? 1006 h
ESC [ ? 25 l
```

(The title line is sent by the client entry `title (OSC 0)`, not by the modes entry.) Client entry
`mode · set / reset` sends one mode at a time.

| Mode / sequence | While active the terminal must | Bytes the terminal sends to the program | Source |
|---|---|---|---|
| `?1049 h/l` | switch to / from the alternate screen (save and restore cursor, clear it on entry) | none | xterm |
| `?25 h/l` | show / hide the cursor | none | xterm |
| `?2004 h/l` bracketed paste | wrap pasted text | `ESC [ 2 0 0 ~` before the text, `ESC [ 2 0 1 ~` after it | xterm ctlseqs |
| `?2031 h/l` colour-scheme reports | report the current scheme whenever it changes | dark: `ESC [ ? 9 9 7 ; 1 n`; light: `ESC [ ? 9 9 7 ; 2 n` | spec (Ghostty change description of mode 2031) |
| `?1004 h/l` focus events | report focus changes | see "Not specified here" | xterm |
| `?1000`, `?1002`, `?1003` | report mouse button events / drags / all motion | see "Not specified here" | xterm |
| `?1006` | use the SGR encoding for mouse reports | see "Not specified here" | xterm |
| `?1016` | never set by Claude Code; only reset on exit and queried | none | xterm |
| `ESC [ > <flags> u` | push `<flags>` on the kitty keyboard flag stack of the current screen | key events are encoded per the flags | spec (kitty keyboard) |
| `ESC [ < u` | pop one stack entry (`ESC [ < <n> u` pops n) | key events follow the new top of stack | spec (kitty keyboard) |
| `ESC [ > 4 ; 2 m` / `ESC [ > 4 m` | set modifyOtherKeys level 2 / reset it | key events are encoded per xterm modifyOtherKeys | spec (xterm) |

Kitty flag bits (spec): 1 disambiguate escape codes, 2 report event types, 4 report alternate keys,
8 report all keys as escape codes, 16 report associated text. Claude Code pushes `5` = 1 + 4. With flag 1,
Shift+Enter is sent as `ESC [ 1 3 ; 2 u` (key code 13, modifier field 1+1 = 2). Implement the rest of the
key encoding from the kitty keyboard specification; this document does not restate it.

Mode state is per terminal and must be idempotent: setting a mode that is set, or resetting one that is
reset, changes nothing.

## 6. Tab title

`ESC ] 0 ; <text> BEL` (**observed**; client entry `title (OSC 0)`). Values seen, in order:

```
ESC ] 0 ; ✳ Claude Code BEL      bytes E2 9C B3 20 43 6C 61 75 64 65 20 43 6F 64 65 (before the queries)
ESC ] 0 ; Claude Code BEL        (right after the queries)
ESC ] 0 ; BEL                    (at exit: empty text)
```

Set the tab/window title to the text. An empty text means the program no longer owns the title; restoring
your default title there is the sensible reading, but the bytes themselves only prove that the title is
emptied at exit.

## 7. Bell and notifications

Claude Code emitted exactly one of these when the permission prompt appeared, depending on its
`preferredNotifChannel` setting (**observed**, one run per value; client entry in parentheses):

| Setting | Bytes | Terminal action |
|---|---|---|
| `terminal_bell` | `BEL` (`bell (BEL)`) | bell / attention marker on the tab |
| `iterm2` | `ESC ] 9 ; Claude needs your permission BEL` (`notify · OSC 9`) | show a notification with that text |
| `kitty` | `ESC ] 99 ; i=2325:d=0:p=title ; Claude Code BEL`<br>`ESC ] 99 ; i=2325:p=body ; Claude needs your permission BEL`<br>`ESC ] 99 ; i=2325:d=1:a=focus ; BEL` (`notify · OSC 99`) | notification titled `Claude Code`, body `Claude needs your permission`; `a=focus`: activating it focuses the window |
| `ghostty` | `ESC ] 777 ; notify ; Claude Code ; Claude needs your permission BEL` (`notify · OSC 777`) | notification with that title and body |

OSC 99: metadata (`key=value` pairs separated by `:`) comes before the first `;`, the payload after it.
`i` identifier, `d` done flag, `p` payload type (`title`, `body`), `a` actions. Follow the kitty
desktop-notification specification for chunking (`d`) when handling the second and third chunk: the
capture shows the second chunk without `d` and the third as an empty payload with `d=1` and `a=focus`, and
this document does not decide how the specification treats that. The `auto` value of the setting was not
captured.

## 8. Exit cleanup (observed)

On the second Ctrl-C Claude Code writes the following (client entry `modes · Claude Code exit`). The same
resets and the `state=clear` report occur several times; handle every one idempotently.

```
ESC [ ? 1006 l  ESC [ ? 1003 l  ESC [ ? 1002 l  ESC [ ? 1000 l  ESC [ ? 25 h
ESC [ ? 1004 l  ESC [ < u  ESC [ ? 1049 l  ESC [ > 4 m  ESC [ > 4 m  ESC [ < u
ESC [ ? 2031 l  ESC [ ? 2004 l  ESC ( B  SI  ESC [ ? 1016 l
ESC [ ? 1006 l  ESC [ ? 1003 l  ESC [ ? 1002 l  ESC [ ? 1000 l  ESC [ ? 25 h
ESC 7  ESC [ r  ESC 8   ESC ] 7501 ; state=clear BEL
ESC [ ? 25 h   ESC ] 7501 ; state=clear BEL
ESC ( B  SI  ESC [ ? 1016 l  ESC [ ? 1006 l  ESC [ ? 1003 l  ESC [ ? 1002 l  ESC [ ? 1000 l
ESC [ > 4 m  ESC [ ? 1004 l  ESC [ ? 2031 l  ESC [ ? 2004 l  ESC [ < u  ESC [ ? 25 h
ESC 7  ESC [ r  ESC 8   ESC ] 7501 ; state=clear BEL   ESC ] 0 ; BEL   ESC [ ? 25 h
```

`ESC 7` / `ESC 8` save / restore the cursor, `ESC [ r` resets the scroll region, `ESC ( B` selects ASCII
into G0, `SI` selects G0. After this the terminal must be back in its pre-session state: main screen,
cursor visible, no mouse/focus/paste/colour-scheme modes, kitty stack empty, modifyOtherKeys off, record
cleared, title restored. Note `?1049 l` appears only once, and the kitty pop `ESC [ < u` more times than
pushes: popping an empty stack must be a no-op.

## 9. Reference code (Go, standard library only)

Tested against the captured bytes: replies in the exact order of section 3 (also when fed 1, 7 or 4096
bytes at a time), the report sequence of section 4, the notification forms of section 7 and the mode state
after section 8. Adapt freely; keep the behaviours.

### parser.go

```go
// Package termside shows what a terminal has to do with the bytes Claude Code
// writes (and which bytes it has to write back). Standard library only.
package termside

// Kind identifies the type of an Event.
type Kind int

const (
	Text Kind = iota // printable bytes (including UTF-8) and C0 controls except BEL/ESC
	Bell             // a bare BEL (0x07) outside any string sequence
	CSI              // ESC [ params intermediates final
	OSC              // ESC ] string (BEL | ESC \)
	APC              // ESC _ string ESC \
	DCS              // ESC P string ESC \
	ESC              // other two-byte ESC sequences
)

// Event is one decoded unit of the output stream.
type Event struct {
	Kind Kind
	// Raw is the exact bytes of the unit, framing included.
	Raw []byte
	// CSI: Private is the optional '<', '=', '>' or '?' that follows ESC [,
	// Params the bytes between it and the intermediates, Inter the
	// intermediate bytes (0x20-0x2F) and Final the final byte (0x40-0x7E).
	Private byte
	Params  string
	Inter   string
	Final   byte
	// OSC/APC/DCS: Body is the string between the introducer and terminator.
	Body string
}

type state int

const (
	sGround state = iota
	sEsc
	sCSI
	sStr // inside OSC / APC / DCS
	sStrEsc
)

// Parser is an incremental decoder; Feed may be called with arbitrary chunk
// boundaries (a sequence split across two reads is completed on the next call).
type Parser struct {
	st    state
	kind  Kind
	raw   []byte
	text  []byte
	flush func(Event)
}

// NewParser returns a Parser that calls emit for every completed event.
func NewParser(emit func(Event)) *Parser { return &Parser{flush: emit} }

func (p *Parser) flushText() {
	if len(p.text) > 0 {
		p.flush(Event{Kind: Text, Raw: p.text})
		p.text = nil
	}
}

// Feed consumes bytes read from the child's output.
func (p *Parser) Feed(b []byte) {
	for _, c := range b {
		switch p.st {
		case sGround:
			switch c {
			case 0x1b:
				p.flushText()
				p.raw = []byte{c}
				p.st = sEsc
			case 0x07:
				p.flushText()
				p.flush(Event{Kind: Bell, Raw: []byte{c}})
			default:
				p.text = append(p.text, c)
			}
		case sEsc:
			p.raw = append(p.raw, c)
			switch c {
			case '[':
				p.st = sCSI
			case ']':
				p.kind, p.st = OSC, sStr
			case '_':
				p.kind, p.st = APC, sStr
			case 'P':
				p.kind, p.st = DCS, sStr
			default:
				p.flush(Event{Kind: ESC, Raw: p.raw})
				p.st = sGround
			}
		case sCSI:
			p.raw = append(p.raw, c)
			if c >= 0x40 && c <= 0x7e {
				p.flush(decodeCSI(p.raw))
				p.st = sGround
			}
		case sStr:
			p.raw = append(p.raw, c)
			if c == 0x1b {
				p.st = sStrEsc
			} else if c == 0x07 && p.kind == OSC { // BEL ends an OSC only
				p.endString(1)
			}
		case sStrEsc:
			p.raw = append(p.raw, c)
			if c == '\\' { // ST
				p.endString(2)
			} else {
				p.st = sStr
			}
		}
	}
	p.flushText()
}

func (p *Parser) endString(termLen int) {
	body := string(p.raw[2 : len(p.raw)-termLen])
	p.flush(Event{Kind: p.kind, Raw: p.raw, Body: body})
	p.st = sGround
}

func decodeCSI(raw []byte) Event {
	ev := Event{Kind: CSI, Raw: raw, Final: raw[len(raw)-1]}
	mid := raw[2 : len(raw)-1]
	if len(mid) > 0 && (mid[0] == '<' || mid[0] == '=' || mid[0] == '>' || mid[0] == '?') {
		ev.Private, mid = mid[0], mid[1:]
	}
	i := 0
	for i < len(mid) && mid[i] >= 0x30 && mid[i] <= 0x3f {
		i++
	}
	ev.Params, ev.Inter = string(mid[:i]), string(mid[i:])
	return ev
}
```

### claude.go

```go
package termside

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// Report is a decoded OSC 7501 status report.
type Report struct {
	State, ID, Kind, App string
	Progress             int // -1 when absent
	Title, Msg           string
}

// ParseReport decodes the body of OSC 7501 (the text after "7501;").
// msg and title are base64 of UTF-8 text.
func ParseReport(body string) (Report, error) {
	r := Report{Progress: -1}
	for _, kv := range strings.Split(body, ":") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return r, fmt.Errorf("pair %q has no '='", kv)
		}
		switch k {
		case "state":
			r.State = v
		case "id":
			r.ID = v
		case "kind":
			r.Kind = v
		case "app":
			r.App = v
		case "progress":
			n, err := strconv.Atoi(v)
			if err != nil {
				return r, err
			}
			r.Progress = n
		case "title", "msg":
			b, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return r, fmt.Errorf("%s: %w", k, err)
			}
			if k == "title" {
				r.Title = string(b)
			} else {
				r.Msg = string(b)
			}
		}
	}
	return r, nil
}

// Notification is what a notification-bearing sequence asks the terminal to show.
type Notification struct {
	Channel string // "bell", "osc9", "osc99", "osc777"
	Title   string
	Body    string
	Meta    string // OSC 99 metadata (between the first and second ';')
}

// Config tells the responder what this terminal supports.
type Config struct {
	Version    string // reported to XTVERSION, e.g. "myterm 1.0"
	KittyFlags int    // current kitty keyboard flags
	CellW      int    // pixels
	CellH      int
	States     []string // for the 7501 query reply
	Kinds      []string
	// DECRQM answers: mode -> 0 unknown, 1 set, 2 reset
	Modes map[int]int
}

// Handler reacts to Claude Code's output. Reply is called with the bytes to
// write to the child's stdin, in the order the queries arrived.
type Handler struct {
	Cfg      Config
	OnReport func(Report)
	OnNotify func(Notification)
	OnTitle  func(string)
	// Modes records which DEC private modes are currently set.
	Modes map[int]bool
	// KittyStack is the kitty keyboard flag stack (CSI > flags u pushes, CSI < u pops).
	KittyStack []int
	Reply      func([]byte)
}

func NewHandler(c Config) *Handler {
	return &Handler{Cfg: c, Modes: map[int]bool{}}
}

func (h *Handler) reply(s string) {
	if h.Reply != nil {
		h.Reply([]byte(s))
	}
}

// Handle is the callback for Parser.
func (h *Handler) Handle(ev Event) {
	switch ev.Kind {
	case Bell:
		if h.OnNotify != nil {
			h.OnNotify(Notification{Channel: "bell"})
		}
	case OSC:
		h.osc(ev.Body)
	case APC:
		// Kitty graphics probe: ESC _ G i=31,s=1,v=1,a=q,... ; AAAA ESC \
		// A terminal without graphics support sends nothing.
	case CSI:
		h.csi(ev)
	}
}

func (h *Handler) osc(body string) {
	num, rest, _ := strings.Cut(body, ";")
	switch num {
	case "0", "2":
		if h.OnTitle != nil {
			h.OnTitle(rest)
		}
	case "9": // OSC 9 ; text
		if h.OnNotify != nil {
			h.OnNotify(Notification{Channel: "osc9", Body: rest})
		}
	case "99": // OSC 99 ; metadata ; payload
		meta, payload, _ := strings.Cut(rest, ";")
		if h.OnNotify != nil {
			h.OnNotify(Notification{Channel: "osc99", Meta: meta, Body: payload,
				Title: titleIf(meta, payload)})
		}
	case "777": // OSC 777 ; notify ; title ; body
		if t, ok := strings.CutPrefix(rest, "notify;"); ok && h.OnNotify != nil {
			title, b, _ := strings.Cut(t, ";")
			h.OnNotify(Notification{Channel: "osc777", Title: title, Body: b})
		}
	case "7501":
		if rest == "?" {
			// Answer before the DA1 reply: in the capture runs, answering DA1
			// first meant no OSC 7501 reports followed.
			h.reply("\x1b]7501;?:states=" + strings.Join(h.Cfg.States, ",") +
				":kinds=" + strings.Join(h.Cfg.Kinds, ",") + "\x1b\\")
			return
		}
		if r, err := ParseReport(rest); err == nil && h.OnReport != nil {
			h.OnReport(r)
		}
	}
}

func titleIf(meta, payload string) string {
	if strings.Contains(meta, "p=title") {
		return payload
	}
	return ""
}

func (h *Handler) csi(ev Event) {
	switch {
	case ev.Final == 'h' || ev.Final == 'l':
		if ev.Private == '?' {
			for _, p := range strings.Split(ev.Params, ";") {
				if n, err := strconv.Atoi(p); err == nil {
					h.Modes[n] = ev.Final == 'h'
				}
			}
		}
	case ev.Final == 'q' && ev.Private == '>': // XTVERSION
		h.reply("\x1bP>|" + h.Cfg.Version + "\x1b\\")
	case ev.Final == 'u' && ev.Private == '?': // kitty keyboard flags query
		h.reply(fmt.Sprintf("\x1b[?%du", h.Cfg.KittyFlags))
	case ev.Final == 'u' && ev.Private == '>': // push
		n, _ := strconv.Atoi(ev.Params)
		h.KittyStack = append(h.KittyStack, h.Cfg.KittyFlags)
		h.Cfg.KittyFlags = n
	case ev.Final == 'u' && ev.Private == '<': // pop
		if k := len(h.KittyStack); k > 0 {
			h.Cfg.KittyFlags, h.KittyStack = h.KittyStack[k-1], h.KittyStack[:k-1]
		}
	case ev.Final == 'c' && ev.Private == 0 && (ev.Params == "" || ev.Params == "0"): // DA1
		h.reply("\x1b[?62c")
	case ev.Final == 'p' && ev.Inter == "$" && ev.Private == '?': // DECRQM
		mode, _ := strconv.Atoi(ev.Params)
		h.reply(fmt.Sprintf("\x1b[?%d;%d$y", mode, h.Cfg.Modes[mode]))
	case ev.Final == 't' && ev.Params == "16": // cell size in pixels
		h.reply(fmt.Sprintf("\x1b[6;%d;%dt", h.Cfg.CellH, h.Cfg.CellW))
	}
}
```

Use: `p := termside.NewParser(h.Handle)`; for every read from the child's output call `p.Feed(buf)`;
set `h.Reply` to a function that writes to the child's stdin.

## 10. Acceptance tests with the `ocs7501` client

Run the client inside the terminal under test. It writes to the controlling tty and, after any command that
expects an answer, listens for 1.5 s and prints a **reply report** in its log: what was expected (name and
byte shape), what was received (exact bytes, as text and hex) and, per expected reply, `found` or
`MISSING`. Verdicts: `reply received`, `partial reply`, `input during wait (not recognised as a reply)`,
`no reply seen`.

| Client entry | Expected verdict | Also check |
|---|---|---|
| `? feature detection` | `reply received` (7501 reply, then DA1) | 7501 reply arrives before the DA1 reply |
| `query · startup burst` | `reply received`: every item found except the optional graphics reply (`not seen` is fine without graphics) | exact bytes in section 3 |
| `query · XTVERSION`, `kitty keyboard`, `device attributes`, `mode (DECRQM)`, `cell size`, `kitty graphics` | `reply received` for supported features, `no reply seen` for unsupported ones (graphics) | reply shapes in section 3 |
| `title (OSC 0)` | tab title becomes `✳ Claude Code` | empty title restores default |
| `bell (BEL)`, `notify · OSC 9`, `notify · OSC 99`, `notify · OSC 777` | bell / one notification with title and body shown | OSC 99 three chunks give one notification |
| `mode · set / reset` | mode state changes; `bracketed paste` then pasting text sends `ESC [ 200 ~ … ESC [ 201 ~` | resets are idempotent |
| `modes · Claude Code startup` then `modes · Claude Code exit` | alternate screen entered then left; mouse/focus/paste modes on then off; cursor visible at end | state identical to before the pair |
| `keyboard · kitty push/pop`, `keyboard · modifyOtherKeys` | key encoding changes accordingly | pop on an empty stack is a no-op |
| `idle` … `clear` | status shown / removed per the OSC 7501 specification | `blocked` with `msg` shows decoded text |

The client prints the sequences it sends in its preview pane, so a failing item can be compared with the
tables above byte by byte. `ocs7501 -out file.bin` writes the same bytes to a file for use as test fixtures.

## Not specified here

* **Focus events (`?1004`) and mouse reports (`?1000`, `?1002`, `?1003`, `?1006`, `?1016`).** Claude Code
  enables them (observed), but the byte formats of the reports a terminal sends were not available when this
  document was written. Take them from the xterm "Control Sequences" document (sections on FocusIn/FocusOut
  and Mouse Tracking) and confirm with the reference of your terminal library.
* What Claude Code does with the replies in section 3 other than the OSC 7501 ordering.
* Output of the `auto` notification setting; `question` and `auth` kinds; the OSC 7501 keys `id`,
  `progress`, `title` as written by Claude Code (the OSC 7501 specification covers them).
* Synchronized output (`?2026`) is only queried by Claude Code in the capture, never set.

## Sources

* Program Status Protocol (OSC 7501): https://www.superlogical.com/rex/docs/build/program-status
* xterm control sequences: https://www.invisible-island.net/xterm/ctlseqs/ctlseqs.html
* kitty keyboard protocol: https://sw.kovidgoyal.net/kitty/keyboard-protocol/
* kitty desktop notifications (OSC 99): https://sw.kovidgoyal.net/kitty/desktop-notifications/
* kitty graphics protocol: https://sw.kovidgoyal.net/kitty/graphics-protocol/
* Claude Code settings reference (`preferredNotifChannel`): https://code.claude.com/docs/en/settings-reference
* Mode 2031 report format: from a Ghostty change description; URL not retained.
