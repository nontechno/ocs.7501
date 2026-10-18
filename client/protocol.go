package main

// Implementation of the sender side of the Program Status Protocol (OSC 7501),
// revision 0.4: https://www.superlogical.com/rex/docs/build/program-status
//
//	OSC 7501 ; pairs ST        report
//	OSC 7501 ; ? ST            feature detection
//
// Everything in this file is independent of the UI so it can be unit tested.

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits from the "Limits" section of the specification.
const (
	maxSequence = 4096 // whole sequence, OSC through ST
	maxMsgEnc   = 2732
	maxMsgDec   = 2048
	maxTitleEnc = 256
	maxTitleDec = 192
	maxIDTotal  = 128
	maxIDSeg    = 32
	maxIDDepth  = 8
)

const oscPrefix = "\x1b]7501;"

// States are the values of the `state` key (not counting "clear").
var States = []string{"idle", "working", "done", "blocked", "error"}

// Kinds are the values of the `kind` key; only meaningful with state=blocked.
var Kinds = []string{"permission", "question", "auth"}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,32}$`)

// Terminator is how the sequence is closed. The spec allows ST (ESC \) or BEL.
type Terminator int

const (
	TermST Terminator = iota
	TermBEL
)

// Bytes returns the raw terminator bytes.
func (t Terminator) Bytes() string {
	if t == TermBEL {
		return "\x07"
	}
	return "\x1b\\"
}

// Label is a human-readable rendering used in the preview.
func (t Terminator) Label() string {
	if t == TermBEL {
		return "BEL"
	}
	return `ESC \`
}

// Report holds the user-facing (not yet encoded) fields of one report.
// Title and Msg are plain text; they are base64-encoded by BuildReport.
type Report struct {
	State    string // idle|working|done|blocked|error|clear
	ID       string // optional record path, e.g. "build/test"
	Kind     string // permission|question|auth (blocked only)
	Progress string // "0".."100" (working/blocked only), "" = indeterminate
	App      string
	Title    string
	Msg      string
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func validateID(id string) error {
	if len(id) > maxIDTotal {
		return fmt.Errorf("id: %d bytes, max %d", len(id), maxIDTotal)
	}
	segs := strings.Split(id, "/")
	if len(segs) > maxIDDepth {
		return fmt.Errorf("id: %d levels deep, max %d", len(segs), maxIDDepth)
	}
	for _, s := range segs {
		if !nameRe.MatchString(s) {
			return fmt.Errorf("id: segment %q must match [A-Za-z0-9_.+-]{1,%d}", s, maxIDSeg)
		}
	}
	return nil
}

// encodeText validates and base64-encodes a free-text value (msg, title).
func encodeText(name, text string, maxEnc, maxDec int) (string, error) {
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("%s: not valid UTF-8", name)
	}
	for _, r := range text {
		// unicode.IsControl is exactly U+0000-U+001F, U+007F, U+0080-U+009F.
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%s: contains control character U+%04X (single line only)", name, r)
		}
	}
	if len(text) > maxDec {
		return "", fmt.Errorf("%s: %d bytes decoded, max %d", name, len(text), maxDec)
	}
	enc := base64.StdEncoding.EncodeToString([]byte(text))
	if len(enc) > maxEnc {
		return "", fmt.Errorf("%s: %d bytes encoded, max %d", name, len(enc), maxEnc)
	}
	return enc, nil
}

// BuildReport turns a Report into the `key=value:key=value` body. It returns
// every validation problem it finds; the body is only meaningful when the
// error list is empty.
//
// Keys the protocol says are ignored in a given state (kind without blocked,
// progress without working/blocked, everything but id for clear) are simply
// not emitted, since "each report replaces its record completely".
func BuildReport(r Report) (string, []string) {
	var errs []string
	state := strings.TrimSpace(r.State)
	if state != "clear" && !contains(States, state) {
		return "", []string{fmt.Sprintf("state: %q is not one of %s, clear", state, strings.Join(States, ", "))}
	}

	pairs := []string{"state=" + state}

	if id := strings.TrimSpace(r.ID); id != "" {
		if err := validateID(id); err != nil {
			errs = append(errs, err.Error())
		} else {
			pairs = append(pairs, "id="+id)
		}
	}

	if state == "clear" {
		return strings.Join(pairs, ":"), errs
	}

	if state == "blocked" {
		if k := strings.TrimSpace(r.Kind); k != "" {
			if !contains(Kinds, k) {
				errs = append(errs, fmt.Sprintf("kind: %q is not one of %s", k, strings.Join(Kinds, ", ")))
			} else {
				pairs = append(pairs, "kind="+k)
			}
		}
	}

	if state == "working" || state == "blocked" {
		if p := strings.TrimSpace(r.Progress); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 || n > 100 {
				errs = append(errs, "progress: must be an integer 0-100 (or empty for indeterminate)")
			} else {
				pairs = append(pairs, "progress="+strconv.Itoa(n))
			}
		}
	}

	if app := strings.TrimSpace(r.App); app != "" {
		if !nameRe.MatchString(app) {
			errs = append(errs, "app: must match [A-Za-z0-9_.+-]{1,32}")
		} else {
			pairs = append(pairs, "app="+app)
		}
	}

	if r.Title != "" {
		if enc, err := encodeText("title", r.Title, maxTitleEnc, maxTitleDec); err != nil {
			errs = append(errs, err.Error())
		} else {
			pairs = append(pairs, "title="+enc)
		}
	}

	if r.Msg != "" {
		if enc, err := encodeText("msg", r.Msg, maxMsgEnc, maxMsgDec); err != nil {
			errs = append(errs, err.Error())
		} else {
			pairs = append(pairs, "msg="+enc)
		}
	}

	return strings.Join(pairs, ":"), errs
}

// QueryBody is the body of the feature-detection query: OSC 7501 ; ? ST.
const QueryBody = "?"

// Sequence wraps a body in OSC 7501 ; ... terminator and enforces the
// 4096-byte whole-sequence limit.
func Sequence(body string, t Terminator) (string, error) {
	s := oscPrefix + body + t.Bytes()
	if len(s) > maxSequence {
		return "", fmt.Errorf("sequence is %d bytes, max %d", len(s), maxSequence)
	}
	return s, nil
}

// PrimaryDA is CSI c. Sending it right after the query lets a program tell
// "no support" from "no reply yet": every terminal answers it.
const PrimaryDA = "\x1b[c"
