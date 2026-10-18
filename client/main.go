// ocs7501 is a terminal UI for sending Program Status Protocol (OSC 7501)
// sequences to the terminal that hosts it. Pick a command in the menu, adjust
// its payload, check the live preview of the exact bytes, and press Enter.
//
// Spec: https://www.superlogical.com/rex/docs/build/program-status
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Palette. The light pastel scheme is the default; -dark (or /dark) switches
// to the dark one. Both are applied by applyTheme before any widget exists.
var (
	cBg, cText, cDim, cBorder, cTitle, cField             tcell.Color
	cLabel, cSel, cMore, cButton, cButtonText, cButtonHot tcell.Color
)

// Colour tags for text views (readable on the chosen background).
var tagDim, tagKey, tagOK, tagErr, tagWarn, tagHead string

func hex(c int32) string { return fmt.Sprintf("[#%06X]", c) }

func applyTheme(dark bool) {
	if dark {
		cBg = tcell.NewHexColor(0x1E1E2E)         // deep blue-grey background
		cText = tcell.NewHexColor(0xCDD6F4)       // soft white text
		cDim = tcell.NewHexColor(0x7F849C)        // secondary text
		cBorder = tcell.NewHexColor(0x585B70)     // muted borders
		cTitle = tcell.NewHexColor(0xB4BEFE)      // lavender titles
		cField = tcell.NewHexColor(0x313244)      // input fields
		cLabel = tcell.NewHexColor(0x89B4FA)      // blue labels
		cSel = tcell.NewHexColor(0x45475A)        // selected list row
		cMore = tcell.NewHexColor(0x313244)       // dropdown popup
		cButton = tcell.NewHexColor(0x2F4A3C)     // green buttons
		cButtonText = tcell.NewHexColor(0xA6E3A1) //
		cButtonHot = tcell.NewHexColor(0x6B4A3A)  // focused button
		tagDim, tagKey, tagOK = hex(0x7F849C), hex(0xFAB387), hex(0xA6E3A1)
		tagErr, tagWarn, tagHead = hex(0xF38BA8), hex(0xF9E2AF), hex(0xB4BEFE)
	} else {
		cBg = tcell.NewHexColor(0xFBF9F5)         // cream background
		cText = tcell.NewHexColor(0x3A3A4C)       // dark slate text
		cDim = tcell.NewHexColor(0x8E8EA3)        // secondary text
		cBorder = tcell.NewHexColor(0xB8A9E0)     // lavender borders
		cTitle = tcell.NewHexColor(0x7A65C7)      // violet titles
		cField = tcell.NewHexColor(0xE3EEFB)      // pastel blue input fields
		cLabel = tcell.NewHexColor(0x4A7FA8)      // steel-blue labels
		cSel = tcell.NewHexColor(0xD8CCF4)        // selected list row
		cMore = tcell.NewHexColor(0xF1E8FA)       // dropdown popup
		cButton = tcell.NewHexColor(0xCFEBDD)     // mint buttons
		cButtonText = tcell.NewHexColor(0x2F5D44) //
		cButtonHot = tcell.NewHexColor(0xFFD9C7)  // peach focused button
		tagDim, tagKey, tagOK = hex(0x8E8EA3), hex(0xB0640F), hex(0x2E8B57)
		tagErr, tagWarn, tagHead = hex(0xC0392B), hex(0xB0640F), hex(0x7A65C7)
	}
	s := &tview.Styles
	s.PrimitiveBackgroundColor = cBg
	s.ContrastBackgroundColor = cField
	s.MoreContrastBackgroundColor = cMore
	s.BorderColor = cBorder
	s.TitleColor = cTitle
	s.GraphicsColor = cBorder
	s.PrimaryTextColor = cText
	s.SecondaryTextColor = cLabel
	s.TertiaryTextColor = cDim
	s.InverseTextColor = cText
	s.ContrastSecondaryTextColor = cDim
}

// normalizeArgs lets the Windows-style switches /dark, /out and /? work next
// to Go's -dark / --dark. Only these exact words are rewritten, so a value
// such as a path after -out is never touched.
func normalizeArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		switch a {
		case "/dark", "/out", "/h", "/help":
			a = "-" + a[1:]
		case "/?":
			a = "-h"
		}
		out[i] = a
	}
	return out
}

type cmdKind int

const (
	kindReport  cmdKind = iota // state=<idle|working|done|blocked|error>
	kindClear                  // state=clear
	kindQuery                  // OSC 7501 ; ? ST
	kindGeneric                // anything defined in inputs.go
)

type command struct {
	name  string
	desc  string
	kind  cmdKind
	state string
	gen   *genCmd // kindGeneric only
}

var commands = append([]command{
	{"idle", "At rest, waiting for the next instruction", kindReport, "idle", nil},
	{"working", "Running; may carry progress 0-100", kindReport, "working", nil},
	{"done", "Finished; result is ready to look at", kindReport, "done", nil},
	{"blocked", "Needs the user: permission, question or auth", kindReport, "blocked", nil},
	{"error", "Failed and stopped", kindReport, "error", nil},
	{"clear", "Remove a record (and children); no id = all", kindClear, "clear", nil},
	{"? feature detection", "Ask the terminal what it supports", kindQuery, "", nil},
}, genCommands...)

// defaultMsgs are the message pre-filled for each state until the user edits
// the msg field themselves.
var defaultMsgs = map[string]string{
	"idle":    "Ready for the next task",
	"working": "Working on the task",
	"done":    "Task finished successfully",
	"blocked": "Approval needed to continue",
	"error":   "Task failed",
}

// defaultProgress is pre-filled for the states that accept progress, until
// the user edits that field.
var defaultProgress = map[string]string{
	"working": "40",
	"blocked": "60",
}

const (
	kindNone     = "(none)"
	defaultApp   = "ocs-client"
	defaultID    = "demo"
	defaultTitle = "OCS Client Demo"
)

// model is everything the user has entered. It outlives command switches so
// shared fields (id, app, ...) carry over when moving between commands.
type model struct {
	cmd       command
	vals      map[string]map[string]string // generic commands: field values per command
	termEdit  bool                         // the user picked a terminator themselves
	rep       Report
	term      Terminator
	csi       bool // also send CSI c after the feature-detection query
	msgEdited bool // once true, msg is no longer replaced by per-state defaults
	progEdit  bool // same for progress
}

// built is the result of turning the model into bytes.
type built struct {
	seq     string   // bytes to write
	extra   string   // bytes written right after seq (CSI c), may be empty
	display string   // tview-formatted, human-readable rendering of seq
	errs    []string // validation problems; seq is empty when non-empty
}

func (m *model) build() built {
	var b built
	var body string
	switch m.cmd.kind {
	case kindReport, kindClear:
		r := m.rep
		r.State = m.cmd.state
		body, b.errs = BuildReport(r)
	case kindQuery:
		body = QueryBody
	case kindGeneric:
		return m.buildGeneric()
	}
	if len(b.errs) > 0 {
		return b
	}
	seq, err := Sequence(body, m.term)
	if err != nil {
		b.errs = []string{err.Error()}
		return b
	}
	b.seq = seq
	b.display = tagKey + "ESC ] 7501 ;[-] " + tview.Escape(body) + " " + tagKey + m.term.Label() + " [-]"
	if m.cmd.kind == kindQuery && m.csi {
		b.extra = PrimaryDA
		b.display += "\n" + tagKey + "ESC [ c[-] " + tagDim + "(primary device attributes)[-]"
	}
	return b
}

// values returns the current field values of a generic command, filling in
// the defaults the first time.
func (m *model) values(c command) map[string]string {
	if m.vals == nil {
		m.vals = map[string]map[string]string{}
	}
	v, ok := m.vals[c.name]
	if !ok {
		v = map[string]string{}
		for _, f := range c.gen.fields {
			v[f.label] = f.def
		}
		m.vals[c.name] = v
	}
	return v
}

func (m *model) buildGeneric() built {
	var b built
	seqs, errs := m.cmd.gen.build(m.values(m.cmd), m.term)
	if len(errs) > 0 {
		b.errs = errs
		return b
	}
	var lines []string
	for _, s := range seqs {
		if len(s) > maxSequence {
			b.errs = []string{fmt.Sprintf("sequence is %d bytes, max %d", len(s), maxSequence)}
			return b
		}
		lines = append(lines, show(s))
	}
	b.seq = strings.Join(seqs, "")
	b.display = strings.Join(lines, "\n")
	return b
}

// show renders raw bytes with ESC and BEL spelled out, escaped for tview.
func show(seq string) string {
	var sb, run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			sb.WriteString(tview.Escape(run.String()))
			run.Reset()
		}
	}
	for i := 0; i < len(seq); i++ {
		switch c := seq[i]; {
		case c == 0x1b:
			flush()
			sb.WriteString(tagKey + "ESC[-] ")
		case c == 0x07:
			flush()
			sb.WriteString(" " + tagKey + "BEL[-]")
		case c == 0x0f:
			flush()
			sb.WriteString(" " + tagKey + "SI[-]")
		case c < 0x20 || c == 0x7f:
			flush()
			sb.WriteString(fmt.Sprintf("%s<0x%02X>[-]", tagKey, c))
		default:
			run.WriteByte(c)
		}
	}
	flush()
	return sb.String()
}

type ui struct {
	app     *tview.Application
	list    *tview.List
	form    *tview.Form
	preview *tview.TextView
	logv    *tview.TextView
	m       *model
	out     io.Writer
	dest    string

	// Best-effort capture of a feature-detection reply. The reply arrives on
	// stdin, which tview owns, so it is seen as key events.
	detectUntil time.Time
	detect      received
	detectName  string
	detectExp   []expect
	tee         *teeTTY // exact input bytes; nil if the tty could not be wrapped
	building    bool    // true while buildForm runs: dropdowns call back on creation
}

func main() {
	outPath := flag.String("out", "", "append sequences to this file/device instead of the controlling terminal (useful for testing)")
	dark := flag.Bool("dark", false, "use the dark colour scheme (also /dark)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [-dark|/dark] [-out path]\n\nSends Program Status Protocol (OSC 7501) sequences to the hosting terminal.\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.CommandLine.Parse(normalizeArgs(os.Args[1:]))

	var out io.Writer
	var closeOut func()
	var dest string
	if *outPath != "" {
		f, err := os.OpenFile(*outPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot open output:", err)
			os.Exit(1)
		}
		out, closeOut, dest = f, func() { f.Close() }, *outPath
	} else {
		out, closeOut, dest = openTTY()
	}
	defer closeOut()

	applyTheme(*dark) // must run before any primitive is created

	u := &ui{
		app: tview.NewApplication(),
		m: &model{
			rep:  Report{ID: defaultID, App: defaultApp, Title: defaultTitle, Kind: "permission"},
			term: TermST,
			csi:  true,
		},
		out:  out,
		dest: dest,
	}
	u.layout()
	u.installTee()

	if err := u.app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func (u *ui) layout() {
	header := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	header.SetText(fmt.Sprintf("%s[::b]Program Status Protocol · OSC 7501[::-][-]   %soutput: %s[-]", tagHead, tagDim, u.dest))

	u.list = tview.NewList().ShowSecondaryText(true)
	u.list.SetBorder(true).SetTitle(" Commands ")
	u.list.SetMainTextColor(cText)
	u.list.SetSecondaryTextColor(cDim)
	u.list.SetShortcutColor(cLabel)
	u.list.SetSelectedTextColor(cText)
	u.list.SetSelectedBackgroundColor(cSel)
	// Shortcuts 1-9, then letters (q is taken by Quit).
	const shortcuts = "123456789abcdefghijklmnoprstuvwxyz"
	for i, c := range commands {
		var sc rune
		if i < len(shortcuts) {
			sc = rune(shortcuts[i])
		}
		u.list.AddItem(c.name, c.desc, sc, nil)
	}
	// No selected-func: Enter is handled globally (it sends), and number
	// shortcuts should only move the selection.
	u.list.SetChangedFunc(func(i int, _, _ string, _ rune) { u.selectCommand(i) })
	u.list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyRune && (ev.Rune() == 'q' || ev.Rune() == 'Q') {
			u.app.Stop()
			return nil
		}
		return ev
	})

	u.form = tview.NewForm()
	u.form.SetBorder(true)
	u.form.SetFieldBackgroundColor(cField)
	u.form.SetFieldTextColor(cText)
	u.form.SetLabelColor(cLabel)
	u.form.SetButtonBackgroundColor(cButton)
	u.form.SetButtonTextColor(cButtonText)
	u.form.SetButtonActivatedStyle(tcell.StyleDefault.Background(cButtonHot).Foreground(cText))

	u.preview = tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	u.preview.SetBorder(true).SetTitle(" Sequence preview ")

	u.logv = tview.NewTextView().SetDynamicColors(true).SetScrollable(true).SetWrap(true)
	u.logv.SetBorder(true).SetTitle(" Sent ")

	footer := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	footer.SetText(tagDim + "↑/↓ choose · Enter send · Tab/F2 payload · F3 scroll log · F1 menu · Ctrl-S/F5 send · q/Ctrl-C quit[-]")

	right := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.form, 0, 1, false).
		AddItem(u.preview, 9, 0, false)

	body := tview.NewFlex().
		AddItem(u.list, 40, 0, true).
		AddItem(right, 0, 1, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(u.logv, 12, 0, false).
		AddItem(footer, 1, 0, false)

	u.app.SetInputCapture(u.handleKey)

	u.app.SetRoot(root, true).SetFocus(u.list)
	u.logf("%swriting sequences to %s[-]", tagDim, u.dest)
	u.selectCommand(0)
}

// handleKey implements the global key bindings.
func (u *ui) handleKey(ev *tcell.EventKey) *tcell.EventKey {
	if ev.Key() == tcell.KeyCtrlC {
		return ev
	}
	if time.Now().Before(u.detectUntil) {
		u.noteDetectKey(ev)
		return nil
	}
	focus := u.app.GetFocus()
	switch ev.Key() {
	case tcell.KeyEnter:
		// Enter sends the selected command from the menu or from any text
		// field. Dropdowns, the checkbox and buttons keep their own Enter.
		if focus == u.list {
			u.send()
			return nil
		}
		if _, ok := focus.(*tview.InputField); ok {
			u.send()
			return nil
		}
	case tcell.KeyTab:
		if focus == u.list {
			u.app.SetFocus(u.form)
			return nil
		}
	case tcell.KeyCtrlS, tcell.KeyF5:
		u.send()
		return nil
	case tcell.KeyF1:
		u.app.SetFocus(u.list)
		return nil
	case tcell.KeyF3:
		u.app.SetFocus(u.logv) // scroll long reports with the arrow keys / PgUp / PgDn
		return nil
	case tcell.KeyF2:
		u.app.SetFocus(u.form)
		return nil
	}
	return ev
}

func (u *ui) selectCommand(i int) {
	c := commands[i]
	u.m.cmd = c
	if c.kind == kindReport {
		if !u.m.msgEdited {
			u.m.rep.Msg = defaultMsgs[c.state]
		}
		if !u.m.progEdit {
			u.m.rep.Progress = defaultProgress[c.state]
		}
	}
	if !u.m.termEdit {
		u.m.term = TermST
		if c.kind == kindGeneric {
			u.m.term = c.gen.defTerm
		}
	}
	u.form.SetTitle(" Payload · " + c.name + " ")
	u.buildForm()
	u.refresh()
}

func (u *ui) addDropDown(label string, opts []string, cur int, selected func(string, int)) {
	u.form.AddDropDown(label, opts, cur, selected)
	if dd, ok := u.form.GetFormItemByLabel(label).(*tview.DropDown); ok {
		dd.SetListStyles(
			tcell.StyleDefault.Background(cMore).Foreground(cText),
			tcell.StyleDefault.Background(cSel).Foreground(cText),
		)
	}
}

// buildForm rebuilds the payload form with only the fields that are
// meaningful for the selected command.
func (u *ui) buildForm() {
	f := u.form
	m := u.m
	f.Clear(true)
	u.building = true
	defer func() { u.building = false }()

	input := func(label string, get func() string, set func(string)) {
		f.AddInputField(label, get(), 0, nil, func(t string) { set(t); u.refresh() })
	}

	switch m.cmd.kind {
	case kindReport:
		input("id (optional)", func() string { return m.rep.ID }, func(s string) { m.rep.ID = s })
		if m.cmd.state == "blocked" {
			opts := append([]string{kindNone}, Kinds...)
			cur := 0
			for i, o := range opts {
				if o == m.rep.Kind {
					cur = i
				}
			}
			u.addDropDown("kind", opts, cur, func(o string, _ int) {
				if o == kindNone {
					o = ""
				}
				m.rep.Kind = o
				u.refresh()
			})
		}
		if m.cmd.state == "working" || m.cmd.state == "blocked" {
			input("progress 0-100", func() string { return m.rep.Progress }, func(s string) { m.rep.Progress = s; m.progEdit = true })
		}
		input("app", func() string { return m.rep.App }, func(s string) { m.rep.App = s })
		input("title", func() string { return m.rep.Title }, func(s string) { m.rep.Title = s })
		input("msg", func() string { return m.rep.Msg }, func(s string) { m.rep.Msg = s; m.msgEdited = true })
	case kindClear:
		input("id (empty = all)", func() string { return m.rep.ID }, func(s string) { m.rep.ID = s })
	case kindQuery:
		f.AddCheckbox("also send CSI c", m.csi, func(c bool) { m.csi = c; u.refresh() })
	case kindGeneric:
		vals := m.values(m.cmd)
		for _, fl := range m.cmd.gen.fields {
			fl := fl
			switch fl.kind {
			case fInput:
				f.AddInputField(fl.label, vals[fl.label], 0, nil, func(t string) { vals[fl.label] = t; u.refresh() })
			case fDrop:
				cur := 0
				for i, o := range fl.opts {
					if o == vals[fl.label] {
						cur = i
					}
				}
				u.addDropDown(fl.label, fl.opts, cur, func(o string, _ int) { vals[fl.label] = o; u.refresh() })
			}
		}
	}

	if m.cmd.kind != kindGeneric || m.cmd.gen.usesTerm {
		term := 0
		if m.term == TermBEL {
			term = 1
		}
		u.addDropDown("terminator", []string{`ST  (ESC \)`, "BEL (0x07)"}, term, func(_ string, i int) {
			m.term = Terminator(i)
			if !u.building {
				m.termEdit = true // only a real user choice stops per-command defaults
			}
			u.refresh()
		})
	}

	f.AddButton("Send", u.send)
	f.AddButton("Quit", u.app.Stop)
}

func (u *ui) refresh() {
	if u.preview == nil {
		return
	}
	b := u.m.build()
	var sb strings.Builder
	if len(b.errs) > 0 {
		for _, e := range b.errs {
			sb.WriteString(tagErr + "✗ " + tview.Escape(e) + "[-]\n")
		}
		sb.WriteString(tagDim + "fix the above to enable sending[-]")
	} else {
		sb.WriteString(b.display)
		sb.WriteString(fmt.Sprintf("\n\n%s%d of %d bytes[-]", tagDim, len(b.seq)+len(b.extra), maxSequence))
	}
	u.preview.SetText(sb.String())
}

func (u *ui) send() {
	b := u.m.build()
	if len(b.errs) > 0 {
		u.logf("%snot sent:[-] %s", tagErr, tview.Escape(strings.Join(b.errs, "; ")))
		return
	}
	if _, err := io.WriteString(u.out, b.seq+b.extra); err != nil {
		u.logf("%swrite failed:[-] %s", tagErr, tview.Escape(err.Error()))
		return
	}
	u.logf("%ssent %s[-] %s", tagOK, tview.Escape(u.m.cmd.name), strings.ReplaceAll(b.display, "\n", " "))

	if exps := u.m.expectations(); len(exps) > 0 {
		u.detect = received{}
		if u.tee != nil {
			u.tee.begin()
		}
		u.detectName = u.m.cmd.name
		u.detectExp = exps
		u.detectUntil = time.Now().Add(1500 * time.Millisecond)
		u.logf("%slistening for a reply for 1.5s (keys are ignored meanwhile)…[-]", tagDim)
		go func() {
			time.Sleep(1600 * time.Millisecond)
			u.app.QueueUpdateDraw(u.finishDetect)
		}()
	}
	if u.m.cmd.kind == kindGeneric && u.m.cmd.gen.redraw {
		// The sequences changed screen/mode state behind tview's back.
		go u.app.QueueUpdateDraw(func() { u.app.Sync() })
	}
}

// expectations lists the replies the selected command should provoke.
func (m *model) expectations() []expect {
	switch m.cmd.kind {
	case kindQuery:
		exps := []expect{ex7501}
		if m.csi {
			exps = append(exps, exDA1)
		}
		return exps
	case kindGeneric:
		if m.cmd.gen.expects != nil {
			return m.cmd.gen.expects(m.values(m.cmd))
		}
	}
	return nil
}

// noteDetectKey records input that arrives while waiting for a reply. The
// terminal's reply comes in on stdin, so tcell surfaces it as key events.
func (u *ui) noteDetectKey(ev *tcell.EventKey) {
	if u.tee == nil {
		u.detect.add(keyToken(ev))
	}
}

func (u *ui) finishDetect() {
	if u.tee != nil {
		u.detect.setRaw(u.tee.end())
	}
	ms := evaluate(u.detectExp, u.detect.bytes())
	out := classify(ms, &u.detect)

	var head string
	switch out {
	case outOK:
		head = tagOK + "reply received:[-] " + tview.Escape(u.detectName)
	case outPartial:
		if ms[0].found {
			head = tagWarn + "partial reply:[-] " + tview.Escape(u.detectName) + " — some expected replies are missing"
			break
		}
		fallthrough
	case outUnrecognised:
		head = tagWarn + "input during wait (not recognised as a reply):[-] " + tview.Escape(u.detectName)
	default:
		head = tagWarn + "no reply seen:[-] " + tview.Escape(u.detectName)
	}
	var sb strings.Builder
	sb.WriteString(head)

	sb.WriteString("\n  expected, and what was found:")
	for i, mt := range ms {
		opt := ""
		if mt.exp.optional {
			opt = " (optional)"
		}
		var res string
		switch {
		case out == outNothing:
			res = tagErr + "nothing received[-]"
		case mt.found && mt.outOfOrder:
			res = tagWarn + "found out of order:[-] " + tview.Escape(mt.text)
		case mt.found:
			res = tagOK + "found:[-] " + tview.Escape(mt.text)
		case mt.exp.optional:
			res = tagDim + "not seen[-]"
		default:
			res = tagErr + "MISSING[-]"
		}
		fmt.Fprintf(&sb, "\n    %d. %s%s — %s  ⇒ %s", i+1, tview.Escape(mt.exp.name), opt, tview.Escape(mt.exp.shape), res)
	}

	if u.detect.empty() {
		sb.WriteString("\n  received: nothing within 1.5 s")
	} else {
		if u.detect.rawMode {
			fmt.Fprintf(&sb, "\n  received: %d byte(s), exactly as read from the terminal", u.detect.count())
		} else {
			fmt.Fprintf(&sb, "\n  received: %d key event(s) rebuilt into %d byte(s)", u.detect.count(), len(u.detect.bytes()))
		}
		fmt.Fprintf(&sb, "\n    text: %s", tview.Escape(u.detect.readable()))
		fmt.Fprintf(&sb, "\n    hex:  %s", tview.Escape(u.detect.hex()))
	}
	for _, d := range diagnose(ms, &u.detect) {
		sb.WriteString("\n  " + tagDim + "note: " + tview.Escape(d) + "[-]")
	}
	if strings.Count(sb.String(), "\n") > 8 {
		sb.WriteString("\n  " + head) // the log follows the tail; repeat the verdict there
	}
	u.logf("%s", sb.String())
}

func (u *ui) logf(format string, args ...any) {
	fmt.Fprintf(u.logv, "%s%s[-] %s\n", tagDim, time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	u.logv.ScrollToEnd()
}
