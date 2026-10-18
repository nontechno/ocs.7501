# Claude Code terminal interactions beyond OSC 7501

Oct 18, 2026 · @Pavel

## What a terminal receives from Claude Code, byte for byte

Claude Code 2.1.302 writes a fixed set of escape sequences besides OSC 7501: a burst of queries at startup, mode switches, a tab title, and one of four notification encodings. This page lists each one as bytes, says what to send back or do, and gives Go code that does it.

**Method.** I ran `claude` 2.1.302 in a pseudo-terminal sized 120 columns by 40 rows, with an empty config directory and a local mock API server. The mock returned one Bash tool call (`touch /tmp/capwork/mockfile.txt`) and then the text `Mock reply: done.`. I recorded every byte Claude Code wrote, through a full session and a double Ctrl-C exit. The capture tool answered queries itself; when it answered DA1 before the OSC 7501 query, Claude Code sent no OSC 7501 reports, and when it answered the 7501 query first, they appeared.

**Labels.** *Observed* means the bytes are in those captures. *Spec* means the byte format of a reply or of a terminal-side event comes from a published specification (see Sources), not from Claude Code. Only one reply-ordering effect was tested against Claude Code itself (startup section). Escape sequences are written as `ESC` = 0x1B, `BEL` = 0x07, `ST` = `ESC \`.

Go examples use only the standard library. They were compiled, vetted and run against the capture files; test output is shown below.

## Startup: queries to answer, in order

After drawing its first screen Claude Code writes 12 sequences back to back (observed, 2.1.302). Answer them in this order, without waiting for any input from the program.

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
ESC [ 16 t
ESC [ ? 1016 $ p
ESC [ c
```

The last `ESC [ c` closes the burst (an earlier one already follows the 7501 query). A query your terminal does not support should get no answer. The one ordering effect I tested directly: with the DA1 reply sent before the 7501 reply, no OSC 7501 reports followed. Whether Claude Code uses the other replies, and how it treats a missing one, I did not test.

| Claude Code writes | Meaning | Reply to write to its stdin | Source |
| --- | --- | --- | --- |
| `ESC [ > 0 q` | XTVERSION | `ESC P > \| myterm 1.0 ST` | Spec (xterm) |
| `ESC [ ? u` | current kitty keyboard flags | `ESC [ ? 0 u` (0 = none set) | Spec (kitty) |
| `ESC ] 7501 ; ? BEL` | OSC 7501 feature detection | `ESC ] 7501 ; ? : states=idle,working,done,blocked,error,clear : kinds=permission,question,auth ESC \` (no spaces) | Spec (OSC 7501) |
| `ESC [ c` | primary device attributes | any DA1 reply, e.g. `ESC [ ? 6 2 c` | Spec (xterm) |
| `ESC [ < u` | pop kitty keyboard flags | none | Spec (kitty) |
| `ESC [ > 5 u` | push kitty flags 5 = 1 (disambiguate) + 4 (alternate keys) | none; from now on send keys as the kitty encoding asks | Spec (kitty) |
| `ESC [ > 4 ; 2 m` | xterm modifyOtherKeys level 2 | none | Spec (xterm) |
| `ESC [ ? 2026 $ p` | DECRQM for synchronized output | `ESC [ ? 2026 ; Ps $ y` with Ps 1 set, 2 reset, 0 unknown | Spec (xterm) |
| `ESC _ G i=31,… ; AAAA ESC \` | kitty graphics probe, 1×1 RGB pixel | `ESC _ G i=31 ; OK ESC \` if supported, otherwise nothing | Spec (kitty graphics) |
| `ESC [ 1 6 t` | cell size in pixels | `ESC [ 6 ; height ; width t` | Spec (xterm) |
| `ESC [ ? 1016 $ p` | DECRQM for SGR pixel mouse | `ESC [ ? 1016 ; Ps $ y` | Spec (xterm) |

Example, for a terminal with 9×18 pixel cells that supports none of the optional features, the exact bytes to write back are, in order:

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

The Go code below produces exactly this list from the captured bytes.

## OSC 7501 reports and the tab title, as written

Claude Code ends every OSC 7501 report with `BEL` (0x07), not `ST`, so your parser must accept both terminators. In the captured session it sent only the keys `state`, `app`, `kind` and `msg`: no `id`, no `progress`, no `title`.

| When (observed) | Bytes written | Decoded |
| --- | --- | --- |
| Right after the startup queries | `ESC ] 7501 ; state=idle:app=claude-code BEL` | idle |
| Prompt submitted, model running | `ESC ] 7501 ; state=working:app=claude-code BEL` | working |
| Permission prompt shown | `ESC ] 7501 ; state=blocked:app=claude-code:kind=permission:msg=YXBwcm92ZSBCYXNoOiB0b3VjaCAvdG1wL2NhcHdvcmsvbW9ja2ZpbGUudHh0 BEL` | blocked, kind permission, msg `approve Bash: touch /tmp/capwork/mockfile.txt` |
| After the prompt, tool running | `ESC ] 7501 ; state=working:app=claude-code BEL` | working |
| Turn finished | `ESC ] 7501 ; state=done:app=claude-code:msg=TW9jayByZXBseTogZG9uZS4= BEL` | done, msg `Mock reply: done.` |
| Exit (written three times) | `ESC ] 7501 ; state=clear BEL` | remove the record |

A report replaces the previous one for the same `id` (none here, so there is one record). `msg` is standard base64 of UTF-8; decode it before display.

**Tab title.** Claude Code also sets the title with OSC 0, terminated by `BEL`. Observed values, in order:

```
ESC ] 0 ; ✳ Claude Code BEL      (bytes E2 9C B3 20 43 6C 61 75 64 65 20 43 6F 64 65; before the queries)
ESC ] 0 ; Claude Code BEL        (right after the startup queries)
ESC ] 0 ; BEL                    (at exit: empty title)
```

Set the tab title to the string after `0;`. An empty string is the program saying it no longer owns the title, so restore whatever your terminal shows by default (my reading; the bytes only show that the title is emptied at exit).

## Modes Claude Code switches on and off

Before the queries Claude Code writes these, in this order (observed): `ESC [ ? 25 h`, `ESC [ ? 2004 h`, `ESC [ ? 2031 h`, `ESC [ ? 1004 h`, the title, `ESC [ ? 1049 h`, `ESC [ 2 J`, `ESC [ H`, then `ESC [ ? 1000 h`, `ESC [ ? 1002 h`, `ESC [ ? 1003 h`, `ESC [ ? 1006 h`, `ESC [ ? 25 l`. Track each one; the right-hand column is what your terminal must do while the mode is set.

| Mode (observed set / reset) | What your terminal does while set | Source of the byte format |
| --- | --- | --- |
| `?1049` alternate screen | Switch to the alternate screen; restore the main screen on reset | xterm |
| `?25` | `h` shows the cursor, `l` hides it | xterm |
| `?2004` bracketed paste | Wrap pasted text: `ESC [ 2 0 0 ~` before it and `ESC [ 2 0 1 ~` after it | xterm (ctlseqs) |
| `?2031` colour-scheme notifications | Send `ESC [ ? 997 ; 1 n` when the scheme is dark and `ESC [ ? 997 ; 2 n` when it is light, whenever it changes | Spec (Ghostty change description) |
| `?1004` focus events | Send a focus-in and a focus-out report when the pane gains and loses focus | Mode confirmed in xterm ctlseqs; I could not retrieve the byte text of the reports here, so I do not quote it |
| `?1000`, `?1002`, `?1003` | Report mouse button events, then drags, then all motion | xterm ctlseqs names the modes; report bytes not retrieved here |
| `?1006` SGR encoding | Use the SGR form for the mouse reports above | xterm ctlseqs names the mode; format not retrieved here |
| `?1016` | Only ever reset by Claude Code (and queried with DECRQM); never set in the capture | xterm |
| `ESC [ > 5 u` / `ESC [ < u` | Push / pop the kitty keyboard flag stack | Spec (kitty) |
| `ESC [ > 4 ; 2 m` / `ESC [ > 4 m` | Set / reset modifyOtherKeys | Spec (xterm) |

With kitty flag 1 pushed, the kitty spec encodes Shift+Enter as `ESC [ 1 3 ; 2 u` (key 13, modifiers 1+1). That is the only key example I list, because it is the one I can source.

**Exit.** On the second Ctrl-C Claude Code writes the following, in this order (observed; line breaks added by me). The resets and the \`state=clear\` report each appear more than once.

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

`ESC 7`, `ESC [ r`, `ESC 8` save the cursor, reset the scroll region and restore the cursor; `ESC ( B` selects ASCII into G0 and `SI` (0x0F) selects G0. Mode resets are repeated, and `?1049 l` appears only once, so treat every reset as idempotent. The Go handler below does this: after the captured session every mode in its table is off except the cursor.

## Notifications: four encodings, chosen by one setting

When the permission prompt appeared (the same moment as `state=blocked`), Claude Code wrote exactly one of these, depending on its `preferredNotifChannel` setting (observed, one run per value):

| Setting value | Bytes written | What your terminal does |
| --- | --- | --- |
| `terminal_bell` | `BEL` (0x07), outside any other sequence | Ring the bell / flag the tab |
| `iterm2` | `ESC ] 9 ; Claude needs your permission BEL` | Show a notification with that text |
| `kitty` | `ESC ] 99 ; i=2325:d=0:p=title ; Claude Code BEL`, then `ESC ] 99 ; i=2325:p=body ; Claude needs your permission BEL`, then `ESC ] 99 ; i=2325:d=1:a=focus ; BEL` | Show a notification titled `Claude Code`, body `Claude needs your permission`; `a=focus` asks that activating it focuses the window |
| `ghostty` | `ESC ] 777 ; notify ; Claude Code ; Claude needs your permission BEL` | Show a notification with that title and body |

In the kitty form the metadata is separated from the payload by the first `;` after `99`; `i`, `d`, `p`, `a` are the OSC 99 keys for identifier, done, payload type and actions. I did not verify how a terminal must treat the second chunk (no `d` key) as opposed to the third; read the chunking rules in the OSC 99 specification before relying on a particular reading.

The three OSC forms end with `BEL`, not `ST`. The default value of the setting, `auto`, is not covered here.

The Go handler turns each of the four forms into one `Notification` value, as shown by the test run further down.

## Go: split the stream into sequences

Every item above starts with `ESC` and ends in a final byte or a terminator, and a `read` can cut a sequence anywhere. This parser keeps state between calls, so feeding it one byte at a time gives the same events as feeding it the whole stream (the tests do both). It treats `BEL` as a terminator only inside an OSC string; a `BEL` anywhere else is a bell.

```go
// Package termside: what a terminal does with the bytes Claude Code writes.
package termside

type Kind int

const (
	Text Kind = iota // printable bytes and C0 controls except BEL/ESC
	Bell             // a bare BEL (0x07) outside any string sequence
	CSI              // ESC [ params intermediates final
	OSC              // ESC ] string (BEL | ESC \)
	APC              // ESC _ string ESC \
	DCS              // ESC P string ESC \
	ESC              // other two-byte ESC sequences
)

type Event struct {
	Kind Kind
	Raw  []byte // exact bytes, framing included
	// CSI only: the optional '<' '=' '>' '?' after ESC [, the parameter
	// bytes, the intermediate bytes (0x20-0x2F) and the final byte.
	Private byte
	Params  string
	Inter   string
	Final   byte
	Body    string // OSC/APC/DCS: text between introducer and terminator
}

type state int

const (
	sGround state = iota
	sEsc
	sCSI
	sStr // inside OSC / APC / DCS
	sStrEsc
)

type Parser struct {
	st    state
	kind  Kind
	raw   []byte
	text  []byte
	flush func(Event)
}

func NewParser(emit func(Event)) *Parser { return &Parser{flush: emit} }

func (p *Parser) flushText() {
	if len(p.text) > 0 {
		p.flush(Event{Kind: Text, Raw: p.text})
		p.text = nil
	}
}

// Feed consumes bytes read from the child's output, in any chunking.
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
			if c == '\\' { // ST = ESC \
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

## Go: answer the queries and act on the output

`Handler.Handle` is the callback for the parser above. It calls `Reply` with each answer in arrival order, calls `OnReport` for OSC 7501 reports, `OnNotify` for the four notification forms and `OnTitle` for OSC 0, and keeps the DEC mode table and the kitty flag stack.

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

**Result.** `go vet` is clean and `go test` passes (5 tests); the startup test replays the capture in chunks of 1, 7 and 4096 bytes and gets the same replies each time. Replaying the startup capture prints the replies below, the eight lines from the startup section (the DECRQM answers are `0` because this example config declares no optional mode), followed by the reports:

```
REPLY  "\x1bP>|myterm 1.0\x1b\\"
REPLY  "\x1b[?0u"
REPLY  "\x1b]7501;?:states=idle,working,done,blocked,error,clear:kinds=permission,question,auth\x1b\\"
REPLY  "\x1b[?62c"
REPLY  "\x1b[?2026;0$y"
REPLY  "\x1b[6;18;9t"
REPLY  "\x1b[?1016;0$y"
REPLY  "\x1b[?62c"
REPORT {State:idle App:claude-code Progress:-1}
REPORT {State:working App:claude-code Progress:-1}
REPORT {State:blocked Kind:permission App:claude-code Progress:-1 Msg:approve Bash: touch /tmp/capwork/mockfile.txt}
REPORT {State:working App:claude-code Progress:-1}
REPORT {State:done App:claude-code Progress:-1 Msg:Mock reply: done.}
REPORT {State:clear Progress:-1}   (three times)
```

Replaying the four notification captures yields `Notification{Channel:"osc9", Body:"Claude needs your permission"}`, the three `osc99` chunks with `Meta` `i=2325:d=0:p=title`, `i=2325:p=body` and `i=2325:d=1:a=focus`, `Notification{Channel:"osc777", Title:"Claude Code", Body:"Claude needs your permission"}`, and one `bell` for `terminal_bell`.

## Not confirmed, and sources

These are the gaps. Nothing above depends on them.

- **Focus and mouse report bytes.** Claude Code sets modes 1004, 1000, 1002, 1003 and 1006 (observed). I could not retrieve the xterm text that gives the bytes of the focus-in/out reports or of the SGR mouse reports, so they are not quoted here.
- **Progress bar and synchronized output.** No OSC 9;4 sequence and no `ESC [ ? 2026 h` appeared in the capture; mode 2026 was only queried.
- **Other report kinds.** The capture produced `kind=permission` only. `question` and `auth`, and the keys `id`, `progress` and `title`, were never written.
- **Effect of the replies.** Only the order of the 7501 and DA1 replies was tested against Claude Code. What it does with the XTVERSION, kitty-flags, DECRQM, cell-size and graphics replies was not tested.
- **Scope.** One version (2.1.302), one pseudo-terminal (120×40), one scripted session. The `auto` notification setting was not captured.

Sources: [Program Status Protocol (OSC 7501)](https://www.superlogical.com/rex/docs/build/program-status) · [xterm control sequences](https://www.invisible-island.net/xterm/ctlseqs/ctlseqs.html) · [kitty keyboard protocol](https://sw.kovidgoyal.net/kitty/keyboard-protocol/) · [kitty desktop notifications (OSC 99)](https://sw.kovidgoyal.net/kitty/desktop-notifications/) · [kitty graphics protocol](https://sw.kovidgoyal.net/kitty/graphics-protocol/) · [Claude Code settings reference](https://code.claude.com/docs/en/settings-reference). The mode 2031 report format comes from a Ghostty change description I read earlier in this session; I do not have its URL.
