package ocs7501

import (
	"fmt"
	"strings"
)

// Features is the content of a feature-query reply: what a terminal accepts.
//
// A nil States means the reply carried no "states" key, which the protocol
// defines as exactly idle, working, done, blocked and error. Likewise a nil
// Kinds means permission, question and auth. A non-nil empty slice means the
// key was present and empty.
type Features struct {
	States []string
	Kinds  []string
}

var (
	defaultStates = []string{"idle", "working", "done", "blocked", "error"}
	defaultKinds  = []string{"permission", "question", "auth"}
)

// SupportsState reports whether the terminal accepts state s. StateClear is
// not a state and is always accepted.
func (f Features) SupportsState(s State) bool {
	if s == StateClear {
		return true
	}
	return contains(orDefault(f.States, defaultStates), string(s))
}

// SupportsKind reports whether the terminal accepts kind k.
func (f Features) SupportsKind(k Kind) bool {
	return contains(orDefault(f.Kinds, defaultKinds), string(k))
}

func orDefault(list, def []string) []string {
	if list == nil {
		return def
	}
	return list
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// EncodeReply returns the feature-query reply for f, terminated with ESC \.
// With nil States and Kinds it equals DefaultReply. Entries must be non-empty
// and consist of [A-Za-z0-9_.+-].
func (f Features) EncodeReply() (string, error) {
	var b strings.Builder
	b.WriteString("\x1b]" + Number + ";?")
	for _, l := range []struct {
		key  string
		list []string
	}{{"states", f.States}, {"kinds", f.Kinds}} {
		if l.list == nil {
			continue
		}
		for _, v := range l.list {
			if !validToken(v, MaxSequence) {
				return "", fmt.Errorf("ocs7501: %s entry %q: %w", l.key, v, ErrBadFeatures)
			}
		}
		b.WriteString(":" + l.key + "=" + strings.Join(l.list, ","))
	}
	b.WriteString("\x1b\\")
	if b.Len() > MaxSequence {
		return "", fmt.Errorf("ocs7501: reply is %d bytes: %w", b.Len(), ErrTooLong)
	}
	return b.String(), nil
}

// parseFeatures reads the pairs that follow "?" in a query or reply. Unknown
// keys and pairs that do not parse are ignored.
func parseFeatures(pairs []pair) Features {
	var f Features
	for _, p := range pairs {
		switch p.key {
		case "states":
			f.States = splitList(p.val)
		case "kinds":
			f.Kinds = splitList(p.val)
		}
	}
	return f
}

func splitList(v string) []string {
	out := []string{}
	if v == "" {
		return out
	}
	for _, s := range strings.Split(v, ",") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
