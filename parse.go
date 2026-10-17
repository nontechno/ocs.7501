package ocs7501

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode"
)

type pair struct{ key, val string }

// ParseBody decodes the body of an OSC sequence: everything between "ESC ]"
// and the terminator, e.g. "7501;state=idle:app=codex". The caller (a
// terminal's escape parser) has already stripped the introducer and the
// terminator.
//
// It implements the receiving rules of the specification:
//
//   - pairs that are malformed (no "=", empty or non-[a-z] key, characters
//     outside [A-Za-z0-9_.,+/=-] in the value) are skipped;
//   - unknown keys are ignored; for repeated keys the last value wins;
//   - an unknown or missing state, or an invalid id, makes the report
//     ignored (an error is returned; the report does not fall back to root);
//   - a kind, progress or app that is invalid is treated as absent, as are a
//     kind outside StateBlocked and a progress outside StateWorking and
//     StateBlocked;
//   - a report that exceeds a limit, whose base64 is invalid, or whose
//     decoded title or msg is not UTF-8 or contains a control character, is
//     discarded entirely (an error is returned);
//   - invisible formatting characters (bidirectional overrides, zero-width
//     characters, ...) are removed from title and msg. The joiners U+200C and
//     U+200D are kept, since scripts and emoji need them.
//
// For a StateClear report only the ID is returned.
//
// "7501;?" followed by optional pairs yields Message.Features.
//
// Nothing is applied by ParseBody; whatever it returns is the caller's to
// act on. It never panics.
func ParseBody(body string) (Message, error) {
	if len(body) > MaxSequence-3 { // ESC ] and at least a one-byte terminator
		return Message{}, fmt.Errorf("ocs7501: body is %d bytes: %w", len(body), ErrTooLong)
	}
	payload, ok := strings.CutPrefix(body, Number+";")
	if !ok {
		return Message{}, ErrNotPSP
	}

	if rest, ok := strings.CutPrefix(payload, "?"); ok {
		if rest != "" && rest[0] != ':' {
			return Message{}, ErrNotPSP
		}
		pairs, err := splitPairs(strings.TrimPrefix(rest, ":"))
		if err != nil {
			return Message{}, err
		}
		f := parseFeatures(pairs)
		return Message{Features: &f}, nil
	}

	pairs, err := splitPairs(payload)
	if err != nil {
		return Message{}, err
	}
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		m[p.key] = p.val // last one wins
	}

	// Check everything before building anything.
	var r Report
	r.State = State(m["state"])
	if !IsValidState(r.State) {
		return Message{}, fmt.Errorf("ocs7501: state %q: %w", m["state"], ErrState)
	}
	if id, ok := m["id"]; ok {
		if !ValidID(id) {
			return Message{}, fmt.Errorf("ocs7501: id %q: %w", id, ErrID)
		}
		r.ID = id
	}
	title, hasTitle, err := decodeText(m, "title", MaxTitleEncoded, MaxTitleDecoded)
	if err != nil {
		return Message{}, err
	}
	msg, hasMsg, err := decodeText(m, "msg", MaxMsgEncoded, MaxMsgDecoded)
	if err != nil {
		return Message{}, err
	}

	if r.State == StateClear {
		return Message{Report: &Report{State: StateClear, ID: r.ID}}, nil
	}

	if k := Kind(m["kind"]); r.State == StateBlocked && IsValidKind(k) {
		r.Kind = k
	}
	if r.State == StateWorking || r.State == StateBlocked {
		if n, ok := parseProgress(m["progress"]); ok {
			r.Progress = &n
		}
	}
	if app := m["app"]; ValidApp(app) {
		r.App = app
	}
	if hasTitle {
		r.Title = stripInvisible(title)
	}
	if hasMsg {
		r.Msg = stripInvisible(msg)
	}
	return Message{Report: &r}, nil
}

// splitPairs splits "k=v:k=v" into pairs, skipping malformed ones. It fails
// only when a well-formed key is longer than MaxKeyLen.
func splitPairs(payload string) ([]pair, error) {
	var out []pair
	for _, seg := range strings.Split(payload, ":") {
		k, v, ok := strings.Cut(seg, "=")
		if !ok {
			continue
		}
		k, v = strings.Trim(k, " \t"), strings.Trim(v, " \t")
		if !validKeyChars(k) || !validValueChars(v) {
			continue
		}
		if len(k) > MaxKeyLen {
			return nil, fmt.Errorf("ocs7501: key of %d bytes: %w", len(k), ErrTooLong)
		}
		out = append(out, pair{k, v})
	}
	return out, nil
}

func validKeyChars(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < 'a' || k[i] > 'z' {
			return false
		}
	}
	return true
}

// validValueChars reports whether v is made of [A-Za-z0-9_.,+/=-] only.
func validValueChars(v string) bool {
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '_', c == '.', c == ',', c == '+', c == '/', c == '=', c == '-':
		default:
			return false
		}
	}
	return true
}

// parseProgress accepts 1-3 ASCII digits whose value is at most 100.
func parseProgress(v string) (int, bool) {
	if v == "" || len(v) > 3 {
		return 0, false
	}
	n := 0
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return 0, false
		}
		n = n*10 + int(v[i]-'0')
	}
	return n, n <= 100
}

// decodeText decodes the base64 value of key, if present, and enforces the
// limits and the no-control-characters rule.
func decodeText(m map[string]string, key string, maxEnc, maxDec int) (string, bool, error) {
	v, ok := m[key]
	if !ok {
		return "", false, nil
	}
	if len(v) > maxEnc {
		return "", false, fmt.Errorf("ocs7501: %s is %d bytes encoded: %w", key, len(v), ErrTooLong)
	}
	raw, err := decodeBase64(v)
	if err != nil {
		return "", false, fmt.Errorf("ocs7501: %s: %w", key, err)
	}
	if len(raw) > maxDec {
		return "", false, fmt.Errorf("ocs7501: %s is %d bytes decoded: %w", key, len(raw), ErrTooLong)
	}
	if err := checkText(raw, maxDec); err != nil {
		return "", false, fmt.Errorf("ocs7501: %s: %w", key, err)
	}
	return raw, true, nil
}

// decodeBase64 decodes standard base64 with optional padding.
func decodeBase64(s string) (string, error) {
	stripped := strings.TrimRight(s, "=")
	pad := len(s) - len(stripped)
	if pad > 2 || (pad > 0 && len(s)%4 != 0) || strings.Contains(stripped, "=") {
		return "", ErrBase64
	}
	b, err := base64.RawStdEncoding.DecodeString(stripped)
	if err != nil {
		return "", ErrBase64
	}
	return string(b), nil
}

// stripInvisible removes characters that can disguise text: format
// characters (Unicode category Cf, which includes bidirectional overrides,
// zero-width spaces and tag characters) except the joiners, and the line and
// paragraph separators.
func stripInvisible(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == 0x200c || r == 0x200d:
			return r
		case r == 0x2028 || r == 0x2029, unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
}
