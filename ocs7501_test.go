package ocs7501

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestEncodeCodexSequences(t *testing.T) {
	// The sequences quoted in task.md.
	cases := []struct {
		r    Report
		want string
	}{
		{Report{State: StateIdle, App: "codex"}, "\x1b]7501;state=idle:app=codex\x1b\\"},
		{Report{State: StateWorking, App: "codex"}, "\x1b]7501;state=working:app=codex\x1b\\"},
		{Report{State: StateBlocked, App: "codex"}, "\x1b]7501;state=blocked:app=codex\x1b\\"},
		{Report{State: StateClear}, "\x1b]7501;state=clear\x1b\\"},
	}
	for _, c := range cases {
		got, err := c.r.Encode()
		if err != nil || got != c.want {
			t.Errorf("Encode(%+v) = %q, %v; want %q", c.r, got, err, c.want)
		}
		m, err := ParseBody(strings.TrimSuffix(strings.TrimPrefix(got, "\x1b]"), "\x1b\\"))
		if err != nil || m.Report == nil || !sameReport(*m.Report, c.r) {
			t.Errorf("round trip of %q = %+v, %v", got, m, err)
		}
	}
}

func sameReport(a, b Report) bool {
	if (a.Progress == nil) != (b.Progress == nil) || (a.Progress != nil && *a.Progress != *b.Progress) {
		return false
	}
	a.Progress, b.Progress = nil, nil
	return a == b
}

func TestEncodeFull(t *testing.T) {
	r := Report{State: StateBlocked, ID: "deploy/eu-west", Kind: KindAuth, Progress: Percent(40),
		App: "brew", Title: "Upgrade", Msg: "Password needed — ünï"}
	got, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b]7501;state=blocked:id=deploy/eu-west:kind=auth:progress=40:app=brew:title=" +
		b64("Upgrade") + ":msg=" + b64("Password needed — ünï") + "\x1b\\"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	m, err := ParseBody(got[2 : len(got)-2])
	if err != nil || !sameReport(*m.Report, r) {
		t.Fatalf("round trip: %+v, %v", m.Report, err)
	}
}

func TestEncodeRejects(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct {
		name string
		r    Report
		err  error
	}{
		{"no state", Report{}, ErrState},
		{"bad state", Report{State: "busy"}, ErrState},
		{"bad id char", Report{State: StateIdle, ID: "a b"}, ErrID},
		{"empty segment", Report{State: StateIdle, ID: "a//b"}, ErrID},
		{"trailing slash", Report{State: StateIdle, ID: "a/"}, ErrID},
		{"long segment", Report{State: StateIdle, ID: long(33)}, ErrID},
		{"deep id", Report{State: StateIdle, ID: "a/b/c/d/e/f/g/h/i"}, ErrID},
		{"long id", Report{State: StateIdle, ID: strings.Repeat(long(31)+"/", 4) + long(5)}, ErrID},
		{"kind not blocked", Report{State: StateWorking, Kind: KindAuth}, ErrNotAllowed},
		{"bad kind", Report{State: StateBlocked, Kind: "x"}, ErrKind},
		{"progress on done", Report{State: StateDone, Progress: Percent(1)}, ErrNotAllowed},
		{"progress 101", Report{State: StateWorking, Progress: Percent(101)}, ErrProgress},
		{"progress -1", Report{State: StateWorking, Progress: Percent(-1)}, ErrProgress},
		{"bad app", Report{State: StateIdle, App: "has space"}, ErrApp},
		{"long app", Report{State: StateIdle, App: long(33)}, ErrApp},
		{"newline msg", Report{State: StateIdle, Msg: "a\nb"}, ErrText},
		{"tab title", Report{State: StateIdle, Title: "a\tb"}, ErrText},
		{"c1 msg", Report{State: StateIdle, Msg: "a\u0085b"}, ErrText},
		{"bad utf8", Report{State: StateIdle, Msg: "\xff"}, ErrText},
		{"long msg", Report{State: StateIdle, Msg: long(2049)}, ErrTooLong},
		{"long title", Report{State: StateIdle, Title: long(193)}, ErrTooLong},
		{"clear with msg", Report{State: StateClear, Msg: "x"}, ErrNotAllowed},
	}
	for _, c := range cases {
		s, err := c.r.Encode()
		if !errors.Is(err, c.err) || s != "" {
			t.Errorf("%s: got %q, %v; want %v", c.name, s, err, c.err)
		}
	}
}

func TestEncodeLimitsAtBoundary(t *testing.T) {
	r := Report{State: StateIdle, Msg: strings.Repeat("a", MaxMsgDecoded), Title: strings.Repeat("b", MaxTitleDecoded)}
	s, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) > MaxSequence {
		t.Fatalf("len %d", len(s))
	}
	// The limits quoted in the spec are the padded base64 lengths.
	if base64.StdEncoding.EncodedLen(MaxMsgDecoded) != MaxMsgEncoded ||
		base64.StdEncoding.EncodedLen(MaxTitleDecoded) != MaxTitleEncoded {
		t.Fatal("encoded limits inconsistent with decoded limits")
	}
	if _, err := ParseBody(s[2 : len(s)-2]); err != nil {
		t.Fatalf("max-size report does not parse: %v", err)
	}
}

func TestValidID(t *testing.T) {
	ok := []string{"a", "build", "build/test", "a.b_c+d-e", "A1", strings.Repeat("a", 32), "a/b/c/d/e/f/g/h"}
	bad := []string{"", "/", "/a", "a/", "a//b", "a b", "a:b", "é", "a/b/c/d/e/f/g/h/i", strings.Repeat("a", 33)}
	for _, id := range ok {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false", id)
		}
	}
	for _, id := range bad {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true", id)
		}
	}
}

func parse(t *testing.T, body string) *Report {
	t.Helper()
	m, err := ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody(%q): %v", body, err)
	}
	if m.Report == nil {
		t.Fatalf("ParseBody(%q): not a report", body)
	}
	return m.Report
}

func parseErr(t *testing.T, body string, want error) {
	t.Helper()
	m, err := ParseBody(body)
	if !errors.Is(err, want) || m.Report != nil || m.Features != nil {
		t.Errorf("ParseBody(%q) = %+v, %v; want %v", body, m, err, want)
	}
}

func TestParseRules(t *testing.T) {
	// Malformed pairs are skipped, the rest is processed.
	r := parse(t, "7501;state=idle:junk:=x:A=b:ke y=1:app=ok:msg=a b:app2=zz")
	if r.State != StateIdle || r.App != "ok" || r.Msg != "" {
		t.Errorf("malformed skip: %+v", r)
	}
	// Unknown keys ignored.
	r = parse(t, "7501;state=idle:future=1:other=yes")
	if r.State != StateIdle {
		t.Errorf("unknown keys: %+v", r)
	}
	// Repeated key: last wins.
	r = parse(t, "7501;state=idle:state=working:app=a:app=b")
	if r.State != StateWorking || r.App != "b" {
		t.Errorf("last wins: %+v", r)
	}
	// Whitespace trimmed.
	r = parse(t, "7501; state = idle : app = x ")
	if r.State != StateIdle || r.App != "x" {
		t.Errorf("trim: %+v", r)
	}
	// Invalid app / kind / progress are treated as absent.
	r = parse(t, "7501;state=blocked:app=bad app:kind=wat:progress=abc")
	if r.State != StateBlocked || r.App != "" || r.Kind != "" || r.Progress != nil {
		t.Errorf("absent: %+v", r)
	}
	for _, p := range []string{"101", "-1", "+5", "1.5", "1000", ""} {
		r = parse(t, "7501;state=working:progress="+p)
		if r.Progress != nil {
			t.Errorf("progress=%q accepted", p)
		}
	}
	r = parse(t, "7501;state=working:progress=0")
	if r.Progress == nil || *r.Progress != 0 {
		t.Errorf("progress=0: %+v", r)
	}
	// kind only with blocked, progress only with working/blocked.
	r = parse(t, "7501;state=working:kind=auth:progress=5")
	if r.Kind != "" || r.Progress == nil {
		t.Errorf("kind/progress gating: %+v", r)
	}
	r = parse(t, "7501;state=done:kind=auth:progress=5")
	if r.Kind != "" || r.Progress != nil {
		t.Errorf("kind/progress gating on done: %+v", r)
	}
	// Clear keeps only the id.
	r = parse(t, "7501;state=clear:id=a/b:app=x:msg="+b64("hi"))
	if *r != (Report{State: StateClear, ID: "a/b"}) {
		t.Errorf("clear: %+v", r)
	}
	// Optional padding.
	r = parse(t, "7501;state=idle:msg="+strings.TrimRight(b64("ab"), "="))
	if r.Msg != "ab" {
		t.Errorf("unpadded: %+v", r)
	}
}

func TestParseIgnored(t *testing.T) {
	parseErr(t, "7501;app=x", ErrState)
	parseErr(t, "7501;state=busy", ErrState)
	parseErr(t, "7501;state=", ErrState)
	// A pair with characters outside the value alphabet is skipped, so the
	// id is absent (root) and the report is still processed.
	if r := parse(t, "7501;state=idle:id=bad id!"); r.ID != "" {
		t.Errorf("got %+v", r)
	}
}

func TestParseInvalidIDIsIgnoredNotRoot(t *testing.T) {
	parseErr(t, "7501;state=idle:id=a//b", ErrID)
	parseErr(t, "7501;state=idle:id=", ErrID)
	parseErr(t, "7501;state=idle:id=a/b/c/d/e/f/g/h/i", ErrID)
}

func TestParseDiscards(t *testing.T) {
	good := b64("hello")
	cases := []struct {
		name, body string
		err        error
	}{
		{"bad base64", "7501;state=idle:app=x:msg=a", ErrBase64},
		{"bad base64 pad", "7501;state=idle:msg=YQ=", ErrBase64},
		{"bad base64 mid pad", "7501;state=idle:msg=Y=Q=", ErrBase64},
		{"control char", "7501;state=idle:msg=" + b64("a\nb"), ErrText},
		{"control char title", "7501;state=idle:title=" + b64("a\x1bb"), ErrText},
		{"esc in msg", "7501;state=idle:msg=" + b64("\x1b[31m"), ErrText},
		{"c1", "7501;state=idle:msg=" + b64("\u009b"), ErrText},
		{"bad utf8", "7501;state=idle:msg=" + base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe}), ErrText},
		{"msg too long decoded", "7501;state=idle:msg=" + b64(strings.Repeat("a", 2049)), ErrTooLong},
		{"title too long decoded", "7501;state=idle:title=" + b64(strings.Repeat("a", 193)), ErrTooLong},
		{"title too long encoded", "7501;state=idle:title=" + strings.Repeat("A", 260), ErrTooLong},
		{"key too long", "7501;state=idle:abcdefghijklmnopq=1", ErrTooLong},
		{"too long body", "7501;state=idle:x=" + strings.Repeat("a", 4100), ErrTooLong},
		{"bad title with good msg", "7501;state=idle:msg=" + good + ":title=a", ErrBase64},
		{"clear still checked", "7501;state=clear:msg=" + b64("a\nb"), ErrText},
	}
	for _, c := range cases {
		parseErr(t, c.body, c.err)
	}
	// Max-length key is fine.
	parse(t, "7501;state=idle:abcdefghijklmnop=1")
}

func TestParseNotPSP(t *testing.T) {
	for _, b := range []string{"", "7501", "0;title", "7502;state=idle", "7501state=idle", "7501;?x"} {
		parseErr(t, b, ErrNotPSP)
	}
}

func TestParseSanitisesInvisible(t *testing.T) {
	in := "a\u202eb\u200bc\u2066d\ufeffe\u2028f\U000e0041g\u200dh"
	r := parse(t, "7501;state=idle:msg="+b64(in)+":title="+b64(in))
	want := "abcdef" + "g\u200dh"
	if r.Msg != want || r.Title != want {
		t.Errorf("got %q / %q, want %q", r.Msg, r.Title, want)
	}
}

func TestParseFeatures(t *testing.T) {
	m, err := ParseBody("7501;?")
	if err != nil || m.Features == nil || m.Features.States != nil || m.Features.Kinds != nil {
		t.Fatalf("query: %+v, %v", m, err)
	}
	m, err = ParseBody("7501;?:states=idle,working,paused:kinds=auth:extra=1:garbage")
	if err != nil || m.Features == nil {
		t.Fatalf("reply: %+v, %v", m, err)
	}
	f := *m.Features
	if len(f.States) != 3 || f.States[2] != "paused" || len(f.Kinds) != 1 {
		t.Errorf("features: %+v", f)
	}
	if !f.SupportsState(StateIdle) || f.SupportsState(StateDone) || !f.SupportsState(StateClear) ||
		!f.SupportsKind(KindAuth) || f.SupportsKind(KindQuestion) {
		t.Errorf("support checks: %+v", f)
	}
	var def Features
	for _, s := range []State{StateIdle, StateWorking, StateDone, StateBlocked, StateError, StateClear} {
		if !def.SupportsState(s) {
			t.Errorf("default must support %s", s)
		}
	}
	if !def.SupportsKind(KindPermission) || !def.SupportsKind(KindQuestion) || !def.SupportsKind(KindAuth) {
		t.Error("default kinds")
	}
}

func TestEncodeReply(t *testing.T) {
	s, err := Features{}.EncodeReply()
	if err != nil || s != DefaultReply || DefaultReply != QuerySequence {
		t.Errorf("default: %q %v", s, err)
	}
	s, err = Features{States: []string{"idle", "done"}, Kinds: []string{}}.EncodeReply()
	if err != nil || s != "\x1b]7501;?:states=idle,done:kinds=\x1b\\" {
		t.Errorf("lists: %q %v", s, err)
	}
	if _, err := (Features{States: []string{"a b"}}).EncodeReply(); !errors.Is(err, ErrBadFeatures) {
		t.Errorf("bad entry: %v", err)
	}
	m, err := ParseBody(strings.TrimSuffix(strings.TrimPrefix(s, "\x1b]"), "\x1b\\"))
	if err != nil || m.Features == nil || m.Features.Kinds == nil || len(m.Features.Kinds) != 0 {
		t.Errorf("reply round trip: %+v %v", m, err)
	}
}

func FuzzParseBody(f *testing.F) {
	for _, s := range []string{"7501;state=idle:app=codex", "7501;?", "7501;state=clear", "7501;?:states=a,b",
		"7501;state=blocked:kind=auth:progress=10:id=a/b:title=YWJj:msg=YWJj", "7501;state=idle:msg=\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, body string) {
		m, err := ParseBody(body)
		if err != nil {
			if m.Report != nil || m.Features != nil {
				t.Fatal("message returned with error")
			}
			return
		}
		if (m.Report == nil) == (m.Features == nil) {
			t.Fatal("exactly one of Report, Features must be set")
		}
		if m.Report != nil {
			// Anything accepted must be re-encodable and stable.
			seq, err := m.Report.Encode()
			if err != nil {
				t.Fatalf("accepted report %+v does not encode: %v", *m.Report, err)
			}
			m2, err := ParseBody(seq[2 : len(seq)-2])
			if err != nil || !sameReport(*m.Report, *m2.Report) {
				t.Fatalf("round trip changed %+v -> %+v (%v)", *m.Report, m2.Report, err)
			}
		}
	})
}

func FuzzEncode(f *testing.F) {
	f.Add("idle", "a/b", "auth", 5, "app", "title", "msg")
	f.Fuzz(func(t *testing.T, st, id, kind string, prog int, app, title, msg string) {
		r := Report{State: State(st), ID: id, Kind: Kind(kind), Progress: &prog, App: app, Title: title, Msg: msg}
		for _, rr := range []Report{r, {State: State(st), ID: id, App: app, Title: title, Msg: msg}} {
			seq, err := rr.Encode()
			if err != nil {
				continue
			}
			if len(seq) > MaxSequence {
				t.Fatal("too long")
			}
			m, err := ParseBody(seq[2 : len(seq)-2])
			if err != nil {
				t.Fatalf("encoded report %q does not parse: %v", seq, err)
			}
			// Parsing removes invisible formatting characters; compare the rest.
			got := *m.Report
			got.Title, got.Msg = rr.Title, rr.Msg
			if !sameReport(got, rr) && rr.State != StateClear {
				t.Fatalf("round trip %+v -> %+v", rr, got)
			}
		}
	})
}
