package main

// Checking what came back from the terminal after a query.
//
// The reply arrives on stdin, which tview/tcell owns, so the program only sees
// key events. keyToken turns each event back into the bytes it most likely
// came from; evaluate compares those bytes with the expected reply patterns.

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

// token is the reconstruction of one key event.
type token struct {
	b    []byte // bytes it stands for (empty when unknown)
	name string // tcell's name for it, set when the bytes are unknown
}

// keyToken rebuilds the input bytes behind a key event.
func keyToken(ev *tcell.EventKey) token {
	var t token
	if ev.Modifiers()&tcell.ModAlt != 0 {
		t.b = append(t.b, 0x1b) // tcell reports ESC+x as Alt+x
	}
	k := ev.Key()
	switch {
	case k == tcell.KeyRune:
		t.b = utf8.AppendRune(t.b, ev.Rune())
	case k < 0x20 || k == 0x7f: // C0 controls: tcell key codes equal the byte
		t.b = append(t.b, byte(k))
	default:
		t.name = ev.Name() // function/cursor keys etc.: bytes unknown
	}
	return t
}

// received is everything seen while waiting. When the program owns a tee'd
// tty (see tee.go) raw holds the exact bytes read from the terminal and
// rawMode is true; otherwise it is rebuilt from key events, which is lossy.
type received struct {
	toks    []token
	raw     []byte
	rawMode bool
}

func (r *received) add(t token) { r.toks = append(r.toks, t) }

// setRaw switches to exact bytes.
func (r *received) setRaw(b []byte) { r.raw, r.rawMode = b, true }

// empty reports whether nothing was received.
func (r *received) empty() bool {
	if r.rawMode {
		return len(r.raw) == 0
	}
	return len(r.toks) == 0
}

// count is the number of bytes (raw mode) or key events.
func (r *received) count() int {
	if r.rawMode {
		return len(r.raw)
	}
	return len(r.toks)
}

// bytes is the concatenation of all known bytes (unknown keys are skipped).
func (r *received) bytes() []byte {
	if r.rawMode {
		return r.raw
	}
	var out []byte
	for _, t := range r.toks {
		out = append(out, t.b...)
	}
	return out
}

func (r *received) hasUnknown() bool {
	if r.rawMode {
		return false
	}
	for _, t := range r.toks {
		if t.name != "" {
			return true
		}
	}
	return false
}

// readable renders bytes with control characters spelled out.
func readable(b []byte) string {
	var sb strings.Builder
	for i, c := range b {
		switch {
		case c == 0x1b:
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString("ESC ")
		case c == 0x07:
			sb.WriteString(" BEL")
		case c == 0x0d:
			sb.WriteString("<CR>")
		case c == 0x0a:
			sb.WriteString("<LF>")
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&sb, "<0x%02X>", c)
		default:
			sb.WriteByte(c)
		}
	}
	return strings.TrimSpace(sb.String())
}

// readableTokens is readable() for the token list, naming unknown keys.
func (r *received) readable() string {
	if r.rawMode {
		return readable(r.raw)
	}
	var parts []string
	var run []byte
	flush := func() {
		if len(run) > 0 {
			parts = append(parts, readable(run))
			run = nil
		}
	}
	for _, t := range r.toks {
		if t.name != "" {
			flush()
			parts = append(parts, "<"+t.name+">")
			continue
		}
		run = append(run, t.b...)
	}
	flush()
	return strings.Join(parts, " ")
}

func (r *received) hex() string {
	var parts []string
	if r.rawMode {
		for _, c := range r.raw {
			parts = append(parts, fmt.Sprintf("%02x", c))
		}
		return strings.Join(parts, " ")
	}
	for _, t := range r.toks {
		if t.name != "" {
			parts = append(parts, "<"+t.name+">")
			continue
		}
		for _, c := range t.b {
			parts = append(parts, fmt.Sprintf("%02x", c))
		}
	}
	return strings.Join(parts, " ")
}

// match is the outcome for one expected reply.
type match struct {
	exp        expect
	found      bool
	outOfOrder bool
	text       string // what matched, readable
}

// evaluate looks for each expected reply, in order first and anywhere second.
func evaluate(exps []expect, got []byte) []match {
	res := make([]match, len(exps))
	pos := 0
	for i, e := range exps {
		res[i].exp = e
		if loc := e.re.FindIndex(got[pos:]); loc != nil {
			res[i].found = true
			res[i].text = readable(got[pos+loc[0] : pos+loc[1]])
			pos += loc[1]
			continue
		}
		if loc := e.re.FindIndex(got); loc != nil {
			res[i].found, res[i].outOfOrder = true, true
			res[i].text = readable(got[loc[0]:loc[1]])
		}
	}
	return res
}

// outcome classifies a check.
type outcome int

const (
	outNothing      outcome = iota // no input at all
	outUnrecognised                // input, but no expected reply found
	outPartial                     // some required replies missing
	outOK                          // every required reply found
)

func classify(ms []match, r *received) outcome {
	if r.empty() {
		return outNothing
	}
	missing, found := 0, 0
	for _, m := range ms {
		switch {
		case m.found:
			found++
		case !m.exp.optional:
			missing++
		}
	}
	switch {
	case missing == 0:
		return outOK
	case found == 0:
		return outUnrecognised
	default:
		return outPartial
	}
}

// diagnose explains, in one or two sentences, why the replies did not match.
func diagnose(ms []match, r *received) []string {
	got := r.bytes()
	var out []string
	var missing []expect
	for _, m := range ms {
		if !m.found && !m.exp.optional {
			missing = append(missing, m.exp)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	switch {
	case r.empty():
		out = append(out, "nothing reached the program: the terminal ignored the query, or the reply was consumed on the way (terminal, multiplexer or ssh layer).")
	default:
		if reDA1.Match(got) {
			notDA1 := false
			for _, e := range missing {
				if e.re != reDA1 {
					notDA1 = true
				}
			}
			if notDA1 {
				out = append(out, "the terminal answered DA1 (ESC [ ? … c) but not the query above it, which is how an unsupported query looks: the terminal ignored it.")
			}
		}
		for _, e := range missing {
			if e.re == re7501 && strings.Contains(string(got), "7501") {
				out = append(out, "the input mentions 7501 but is not ESC ] 7501 ; ? : states=… : kinds=… followed by ESC \\ or BEL.")
			}
		}
		if len(out) == 0 {
			out = append(out, fmt.Sprintf("the input has the shape of none of the expected replies (it starts with byte 0x%02x).", got0(got)))
		}
	}
	if !r.rawMode {
		out = append(out, "bytes are rebuilt from key events that tcell decoded; sequences it recognises (focus, mouse, paste, function keys) can appear as <Name> or be dropped.")
	}
	if r.hasUnknown() {
		out = append(out, "at least one event could not be turned back into bytes and is shown by name.")
	}
	return out
}

func got0(b []byte) byte {
	if len(b) == 0 {
		return 0
	}
	return b[0]
}
