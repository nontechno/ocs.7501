package ocs7501

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Errors returned by Encode, Validate and ParseBody. Returned errors wrap one
// of these; test with errors.Is.
var (
	ErrNotPSP      = errors.New("not an OSC 7501 body")
	ErrState       = errors.New("missing or unknown state")
	ErrID          = errors.New("invalid id")
	ErrKind        = errors.New("invalid kind")
	ErrProgress    = errors.New("invalid progress")
	ErrApp         = errors.New("invalid app")
	ErrText        = errors.New("invalid text (not UTF-8, or contains control characters)")
	ErrBase64      = errors.New("invalid base64")
	ErrTooLong     = errors.New("limit exceeded")
	ErrNotAllowed  = errors.New("key not allowed with this state")
	ErrBadFeatures = errors.New("invalid feature list")
)

// IsValidState reports whether s is one of the six defined state values.
func IsValidState(s State) bool {
	switch s {
	case StateIdle, StateWorking, StateDone, StateBlocked, StateError, StateClear:
		return true
	}
	return false
}

// IsValidKind reports whether k is one of the three defined kinds.
func IsValidKind(k Kind) bool {
	switch k {
	case KindPermission, KindQuestion, KindAuth:
		return true
	}
	return false
}

// ValidID reports whether id is a valid, non-empty record id. (The root
// record has no id at all, which is represented by the empty string and is
// not accepted here.)
func ValidID(id string) bool {
	if id == "" || len(id) > MaxIDLen {
		return false
	}
	depth := 0
	for rest := id; ; {
		seg, tail, more := strings.Cut(rest, "/")
		if !validToken(seg, MaxIDSegmentLen) {
			return false
		}
		depth++
		if depth > MaxIDDepth {
			return false
		}
		if !more {
			return true
		}
		rest = tail
	}
}

// ValidApp reports whether app is a valid, non-empty program identifier.
func ValidApp(app string) bool { return validToken(app, MaxAppLen) }

// validToken reports whether s is 1..max bytes of [A-Za-z0-9_.+-].
func validToken(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '_', c == '.', c == '+', c == '-':
		default:
			return false
		}
	}
	return true
}

// isControl reports whether r is a C0 control, DEL or a C1 control.
func isControl(r rune) bool { return r < 0x20 || (r >= 0x7f && r <= 0x9f) }

// checkText verifies that s is valid UTF-8 without control characters and at
// most max bytes long.
func checkText(s string, max int) error {
	if len(s) > max {
		return ErrTooLong
	}
	if !utf8.ValidString(s) {
		return ErrText
	}
	for _, r := range s {
		if isControl(r) {
			return ErrText
		}
	}
	return nil
}

// Encode returns the complete escape sequence for r, terminated with ESC \.
// It returns an error, and no sequence, if r would not be a legal report:
// unknown state, invalid id or app, kind without StateBlocked, progress
// outside 0-100 or without StateWorking/StateBlocked, text that is not UTF-8
// or contains control characters, or any limit exceeded.
//
// A report with StateClear may only carry an ID.
func (r Report) Encode() (string, error) {
	if !IsValidState(r.State) {
		return "", fmt.Errorf("ocs7501: state %q: %w", r.State, ErrState)
	}
	if r.ID != "" && !ValidID(r.ID) {
		return "", fmt.Errorf("ocs7501: id %q: %w", r.ID, ErrID)
	}
	if r.State == StateClear {
		if r.Kind != "" || r.Progress != nil || r.App != "" || r.Title != "" || r.Msg != "" {
			return "", fmt.Errorf("ocs7501: clear carries only an id: %w", ErrNotAllowed)
		}
	}
	if r.Kind != "" {
		if !IsValidKind(r.Kind) {
			return "", fmt.Errorf("ocs7501: kind %q: %w", r.Kind, ErrKind)
		}
		if r.State != StateBlocked {
			return "", fmt.Errorf("ocs7501: kind needs state blocked: %w", ErrNotAllowed)
		}
	}
	if r.Progress != nil {
		if *r.Progress < 0 || *r.Progress > 100 {
			return "", fmt.Errorf("ocs7501: progress %d: %w", *r.Progress, ErrProgress)
		}
		if r.State != StateWorking && r.State != StateBlocked {
			return "", fmt.Errorf("ocs7501: progress needs state working or blocked: %w", ErrNotAllowed)
		}
	}
	if r.App != "" && !ValidApp(r.App) {
		return "", fmt.Errorf("ocs7501: app %q: %w", r.App, ErrApp)
	}
	if err := checkText(r.Title, MaxTitleDecoded); err != nil {
		return "", fmt.Errorf("ocs7501: title: %w", err)
	}
	if err := checkText(r.Msg, MaxMsgDecoded); err != nil {
		return "", fmt.Errorf("ocs7501: msg: %w", err)
	}

	var b strings.Builder
	b.WriteString("\x1b]" + Number + ";state=")
	b.WriteString(string(r.State))
	if r.ID != "" {
		b.WriteString(":id=" + r.ID)
	}
	if r.Kind != "" {
		b.WriteString(":kind=" + string(r.Kind))
	}
	if r.Progress != nil {
		b.WriteString(":progress=" + strconv.Itoa(*r.Progress))
	}
	if r.App != "" {
		b.WriteString(":app=" + r.App)
	}
	if r.Title != "" {
		b.WriteString(":title=" + base64.StdEncoding.EncodeToString([]byte(r.Title)))
	}
	if r.Msg != "" {
		b.WriteString(":msg=" + base64.StdEncoding.EncodeToString([]byte(r.Msg)))
	}
	b.WriteString("\x1b\\")

	if b.Len() > MaxSequence {
		return "", fmt.Errorf("ocs7501: sequence is %d bytes: %w", b.Len(), ErrTooLong)
	}
	return b.String(), nil
}

// Validate reports whether r can be encoded; see Encode.
func (r Report) Validate() error {
	_, err := r.Encode()
	return err
}
