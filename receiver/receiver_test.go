package receiver

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	ocs7501 "github.com/nontechno/ocs.7501"
)

func seq(t *testing.T, r ocs7501.Report) string {
	t.Helper()
	s, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestScannerCodexStream(t *testing.T) {
	stream := "prompt$ \x1b[1mhello\x1b[0m\r\n" +
		"\x1b]7501;state=idle:app=codex\x1b\\" +
		"text\x1b]0;window title\x07more" +
		"\x1b]7501;state=working:app=codex\x1b\\" +
		"\x1b]7501;state=blocked:app=codex\x07" + // BEL terminator
		"\x1b]7501;state=clear\x1b\\"
	want := []string{"idle", "working", "blocked", "clear"}

	// Whole, and split at every possible position (in two chunks), and byte by byte.
	check := func(name string, chunks [][]byte) {
		var sc Scanner
		var got []string
		for _, c := range chunks {
			for _, m := range sc.Feed(c) {
				got = append(got, string(m.Report.State))
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: got %v want %v", name, got, want)
		}
	}
	b := []byte(stream)
	check("whole", [][]byte{b})
	for i := 0; i <= len(b); i++ {
		check(fmt.Sprintf("split@%d", i), [][]byte{b[:i], b[i:]})
	}
	var one [][]byte
	for i := range b {
		one = append(one, b[i:i+1])
	}
	check("bytewise", one)
}

func TestScannerDropsAndRecovers(t *testing.T) {
	good := "\x1b]7501;state=idle\x1b\\"
	huge := "\x1b]7501;state=idle:msg=" + strings.Repeat("A", 5000) + "\x1b\\"
	cases := map[string]string{
		"invalid report":      "\x1b]7501;state=bogus\x1b\\",
		"over limit":          huge,
		"other osc":           "\x1b]52;c;" + strings.Repeat("A", 6000) + "\x07",
		"cancelled by CAN":    "\x1b]7501;state=working\x18",
		"cancelled by SUB":    "\x1b]7501;state=working\x1a",
		"aborted by new ESC":  "\x1b]7501;state=working\x1bX",
		"unterminated + new":  "\x1b]7501;state=working",
		"csi noise":           "\x1b[38;5;196m\x1b[2J\x1b[" + strings.Repeat("1;", 100) + "m",
		"skip then terminate": "\x1b]9;4;1;50\x1b\\",
	}
	for name, junk := range cases {
		var sc Scanner
		ms := sc.Feed([]byte(junk + good))
		if len(ms) != 1 || ms[0].Report == nil || ms[0].Report.State != ocs7501.StateIdle {
			t.Errorf("%s: got %+v", name, ms)
		}
	}
}

func TestScannerSequenceSizeLimit(t *testing.T) {
	// A report of exactly 4096 bytes in total is accepted, one more is not.
	// body = "7501;state=idle:x=" + padding; total = 2 + len(body) + 2 (ESC \).
	mk := func(total int) string {
		prefix := "7501;state=idle:x="
		return "\x1b]" + prefix + strings.Repeat("a", total-2-2-len(prefix)) + "\x1b\\"
	}
	var sc Scanner
	if ms := sc.Feed([]byte(mk(4096))); len(ms) != 1 {
		t.Errorf("4096 bytes: %+v", ms)
	}
	if ms := sc.Feed([]byte(mk(4097))); len(ms) != 0 {
		t.Errorf("4097 bytes: %+v", ms)
	}
}

func TestScannerQuery(t *testing.T) {
	var sc Scanner
	ms := sc.Feed([]byte(ocs7501.QuerySequence))
	if len(ms) != 1 || ms[0].Features == nil {
		t.Fatalf("%+v", ms)
	}
}

func TestStoreReplacementAndHierarchy(t *testing.T) {
	st := NewStore(0)
	app := func(id, a string, s ocs7501.State, title string) ocs7501.Report {
		return ocs7501.Report{State: s, ID: id, App: a, Title: title}
	}
	if !st.Apply(app("", "deploy", ocs7501.StateWorking, "Deploy")) {
		t.Error("first apply should change")
	}
	if st.Apply(app("", "deploy", ocs7501.StateWorking, "Deploy")) {
		t.Error("identical apply should not report a change")
	}
	st.Apply(app("us-east", "", ocs7501.StateWorking, "US East"))
	st.Apply(app("eu-west", "", ocs7501.StateDone, "EU West"))
	st.Apply(app("eu-west/db", "psql", ocs7501.StateError, "DB"))

	if r, _ := st.Get("us-east"); r.EffectiveApp != "deploy" {
		t.Errorf("inheritance from root: %+v", r)
	}
	if r, _ := st.Get("eu-west/db"); r.EffectiveApp != "psql" || r.App != "psql" {
		t.Errorf("own app: %+v", r)
	}
	// Full replacement: the title is gone.
	st.Apply(ocs7501.Report{State: ocs7501.StateIdle, ID: "us-east"})
	if r, _ := st.Get("us-east"); r.Title != "" || r.State != ocs7501.StateIdle {
		t.Errorf("replacement: %+v", r)
	}
	// Parent need not exist; inheritance skips missing parents.
	st.Apply(ocs7501.Report{State: ocs7501.StateIdle, ID: "x/y/z"})
	if r, _ := st.Get("x/y/z"); r.EffectiveApp != "deploy" {
		t.Errorf("missing parents: %+v", r)
	}

	// Clearing a child removes it and its descendants only.
	if !clearID(st, "eu-west") {
		t.Error("clear should change")
	}
	if _, ok := st.Get("eu-west"); ok {
		t.Error("eu-west should be gone")
	}
	if _, ok := st.Get("eu-west/db"); ok {
		t.Error("descendant should be gone")
	}
	if _, ok := st.Get("us-east"); !ok {
		t.Error("sibling must stay")
	}
	// A prefix that is not a path ancestor must survive.
	st.Apply(ocs7501.Report{State: ocs7501.StateIdle, ID: "build"})
	st.Apply(ocs7501.Report{State: ocs7501.StateIdle, ID: "build2"})
	st.Apply(ocs7501.Report{State: ocs7501.StateIdle, ID: "build/test"})
	clearID(st, "build")
	if _, ok := st.Get("build2"); !ok {
		t.Error("build2 is not a descendant of build")
	}
	if _, ok := st.Get("build/test"); ok {
		t.Error("build/test is a descendant of build")
	}
	// Clearing with no id removes everything.
	st.Apply(ocs7501.Report{State: ocs7501.StateClear})
	if st.Len() != 0 {
		t.Errorf("clear all left %d", st.Len())
	}
	if st.Apply(ocs7501.Report{State: ocs7501.StateClear}) {
		t.Error("clearing nothing is not a change")
	}
}

func clearID(s *Store, id string) bool {
	return s.Apply(ocs7501.Report{State: ocs7501.StateClear, ID: id})
}

func TestStoreLifetimes(t *testing.T) {
	st := NewStore(0)
	for i, s := range []ocs7501.State{ocs7501.StateIdle, ocs7501.StateWorking, ocs7501.StateBlocked, ocs7501.StateDone, ocs7501.StateError} {
		st.Apply(ocs7501.Report{State: s, ID: fmt.Sprintf("r%d", i)})
	}
	if !st.ProgramEnded() {
		t.Error("should change")
	}
	var left []string
	for _, r := range st.Records() {
		left = append(left, string(r.State))
	}
	if strings.Join(left, ",") != "done,error" {
		t.Errorf("left: %v", left)
	}
	if st.ProgramEnded() {
		t.Error("second call must not change anything")
	}
	st.Reset()
	if st.Len() != 0 {
		t.Error("reset")
	}
}

func TestStoreEviction(t *testing.T) {
	st := NewStore(1) // raised to the minimum of 64
	if st.max != ocs7501.MinRecords {
		t.Fatalf("max = %d", st.max)
	}
	for i := 0; i < 64; i++ {
		st.Apply(ocs7501.Report{State: ocs7501.StateIdle, ID: fmt.Sprintf("r%d", i)})
	}
	// Touch r0 so r1 becomes the least recently updated.
	st.Apply(ocs7501.Report{State: ocs7501.StateWorking, ID: "r0"})
	st.Apply(ocs7501.Report{State: ocs7501.StateIdle, ID: "new"})
	if st.Len() != 64 {
		t.Fatalf("len %d", st.Len())
	}
	if _, ok := st.Get("r1"); ok {
		t.Error("r1 should have been evicted")
	}
	if _, ok := st.Get("r0"); !ok {
		t.Error("r0 was updated recently and must stay")
	}
	if NewStore(0).max != 256 {
		t.Error("default is 256")
	}
}

func TestStoreHandle(t *testing.T) {
	st := NewStore(0)
	if reply, ch := st.Handle("7501;state=working:app=cargo:progress=30"); reply != "" || !ch {
		t.Errorf("report: %q %v", reply, ch)
	}
	if reply, ch := st.Handle("7501;?"); reply != ocs7501.DefaultReply || ch {
		t.Errorf("query: %q %v", reply, ch)
	}
	for _, bad := range []string{"", "0;x", "7501;state=nope", "7501;state=idle:msg=Q"} {
		before := st.Len()
		if reply, ch := st.Handle(bad); reply != "" || ch || st.Len() != before {
			t.Errorf("%q: %q %v", bad, reply, ch)
		}
	}
	r, ok := st.Get("")
	if !ok || r.State != ocs7501.StateWorking || *r.Progress != 30 || r.EffectiveApp != "cargo" {
		t.Errorf("%+v", r)
	}
	// Returned records are copies.
	*r.Progress = 99
	if r2, _ := st.Get(""); *r2.Progress != 30 {
		t.Error("record aliasing")
	}
}

func TestStoreRejectsIllegalReports(t *testing.T) {
	st := NewStore(0)
	for _, r := range []ocs7501.Report{{}, {State: "x"}, {State: ocs7501.StateIdle, ID: "a b"}, {State: ocs7501.StateIdle, Msg: "a\nb"}} {
		if st.Apply(r) {
			t.Errorf("accepted %+v", r)
		}
	}
	if st.Len() != 0 {
		t.Error("store modified")
	}
}

func TestConcurrency(t *testing.T) {
	st := NewStore(0)
	var sc Scanner
	var wg sync.WaitGroup
	stream := []byte(seq(t, ocs7501.Report{State: ocs7501.StateWorking, App: "x"}) + "noise\x1b[0m")
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				for _, m := range sc.Feed(stream) {
					st.Apply(*m.Report)
				}
				st.Apply(ocs7501.Report{State: ocs7501.StateDone, ID: fmt.Sprintf("g%d/%d", g, i%10)})
				st.Records()
				st.Get("")
				if i%100 == 0 {
					st.ProgramEnded()
				}
			}
		}(g)
	}
	wg.Wait()
}

func FuzzScanner(f *testing.F) {
	f.Add([]byte("\x1b]7501;state=idle\x1b\\"), []byte("\x1b]7501;?\x07"))
	f.Add([]byte("\x1b]7501;state=wor"), []byte("king\x1b\\\x1b[3"))
	f.Fuzz(func(t *testing.T, a, b []byte) {
		var split, whole Scanner
		var n1, n2 int
		n1 += len(split.Feed(a))
		n1 += len(split.Feed(b))
		n2 += len(whole.Feed(append(append([]byte{}, a...), b...)))
		if n1 != n2 {
			t.Fatalf("chunking changed the result: %d vs %d", n1, n2)
		}
	})
}
