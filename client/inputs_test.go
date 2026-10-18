package main

import (
	"strings"
	"testing"
)

func tokens(b string) received {
	var r received
	for _, c := range []byte(b) {
		r.add(token{b: []byte{c}})
	}
	return r
}

func defaults(c command) map[string]string {
	v := map[string]string{}
	for _, f := range c.gen.fields {
		v[f.label] = f.def
	}
	return v
}

func TestDefaultsBuildCleanly(t *testing.T) {
	for _, c := range genCommands {
		seqs, errs := c.gen.build(defaults(c), c.gen.defTerm)
		if len(errs) > 0 || len(seqs) == 0 {
			t.Errorf("%s: seqs=%q errs=%v", c.name, seqs, errs)
		}
	}
}

// Exact bytes observed from Claude Code 2.1.302 (see claude-server.md).
func TestObservedBytes(t *testing.T) {
	want := map[string]string{
		"title (OSC 0)":    "\x1b]0;✳ Claude Code\x07",
		"bell (BEL)":       "\x07",
		"notify · OSC 9":   "\x1b]9;Claude needs your permission\x07",
		"notify · OSC 99":  "\x1b]99;i=2325:d=0:p=title;Claude Code\x07\x1b]99;i=2325:p=body;Claude needs your permission\x07\x1b]99;i=2325:d=1:a=focus;\x07",
		"notify · OSC 777": "\x1b]777;notify;Claude Code;Claude needs your permission\x07",
		"query · startup burst": "\x1b[>0q\x1b[?u\x1b]7501;?\x07\x1b[c\x1b[<u\x1b[>5u\x1b[>4;2m\x1b[?2026$p" +
			"\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\\x1b[16t\x1b[?1016$p\x1b[c",
	}
	for _, c := range genCommands {
		w, ok := want[c.name]
		if !ok {
			continue
		}
		seqs, errs := c.gen.build(defaults(c), c.gen.defTerm)
		if len(errs) > 0 || strings.Join(seqs, "") != w {
			t.Errorf("%s:\n got %q\nwant %q (%v)", c.name, strings.Join(seqs, ""), w, errs)
		}
		delete(want, c.name)
	}
	for n := range want {
		t.Errorf("command %q missing", n)
	}
}

func TestReplyChecks(t *testing.T) {
	burst := func(c command) []expect { return c.gen.expects(defaults(c)) }
	var b command
	for _, c := range genCommands {
		if c.name == "query · startup burst" {
			b = c
		}
	}
	good := "\x1bP>|myterm 1.0\x1b\\" + "\x1b[?0u" +
		"\x1b]7501;?:states=idle,working,done,blocked,error,clear:kinds=permission,question,auth\x1b\\" +
		"\x1b[?62c" + "\x1b[?2026;0$y" + "\x1b[6;18;9t" + "\x1b[?1016;0$y" + "\x1b[?62c"
	r := tokens(good)
	ms := evaluate(burst(b), r.bytes())
	if classify(ms, &r) != outOK {
		t.Errorf("full reply not OK: %+v", ms)
	}

	// Only DA1 answered: nothing recognised, and the diagnosis says so.
	r = tokens("\x1b[?62c\x1b[?62c")
	ms = evaluate(burst(b), r.bytes())
	if got := classify(ms, &r); got != outPartial {
		t.Errorf("DA1 only: %v", got)
	}
	if d := strings.Join(diagnose(ms, &r), " "); !strings.Contains(d, "answered DA1") {
		t.Errorf("diagnosis: %s", d)
	}

	// A 7501 query answered with DA1 only is "unrecognised".
	r = tokens("\x1b[?62c")
	ms = evaluate([]expect{ex7501}, r.bytes())
	if classify(ms, &r) != outUnrecognised {
		t.Error("expected outUnrecognised")
	}
	if classify(evaluate([]expect{ex7501}, nil), &received{}) != outNothing {
		t.Error("expected outNothing")
	}
	if got := readable([]byte("\x1b[?62c")); got != "ESC [?62c" {
		t.Errorf("readable = %q", got)
	}
}

func TestInputValidation(t *testing.T) {
	for _, c := range genCommands {
		if c.name != "notify · OSC 777" && c.name != "notify · OSC 99" && c.name != "title (OSC 0)" {
			continue
		}
		v := defaults(c)
		for k := range v {
			v[k] = "bad\x1b]0;x"
		}
		if _, errs := c.gen.build(v, TermBEL); len(errs) == 0 {
			t.Errorf("%s accepted control characters", c.name)
		}
	}
}
