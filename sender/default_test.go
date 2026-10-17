package sender

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

	ocs7501 "github.com/nontechno/ocs.7501"
)

// resetDefault returns the package to its initial, undetected state.
func resetDefault(t *testing.T) {
	t.Helper()
	reset := func() {
		def.mu.Lock()
		def.s, def.app, def.title = nil, "", ""
		def.mu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func TestDefaultSendWithDefaults(t *testing.T) {
	resetDefault(t)
	var buf bytes.Buffer
	SetOutput(&buf)
	if !Enabled() {
		t.Fatal("not enabled")
	}
	if err := SetDefaults("mytool", "Deploy"); err != nil {
		t.Fatal(err)
	}

	want := func(r ocs7501.Report, seq string) {
		t.Helper()
		buf.Reset()
		if err := Send(r); err != nil {
			t.Fatal(err)
		}
		if buf.String() != seq {
			t.Errorf("got %q want %q", buf.String(), seq)
		}
	}
	// Root record: app and title filled in.
	want(ocs7501.Report{State: ocs7501.StateWorking},
		"\x1b]7501;state=working:app=mytool:title=RGVwbG95\x1b\\")
	// The report's own values win.
	want(ocs7501.Report{State: ocs7501.StateIdle, App: "other", Title: "T"},
		"\x1b]7501;state=idle:app=other:title=VA==\x1b\\")
	// A child gets the app but not the parent's title.
	want(ocs7501.Report{State: ocs7501.StateDone, ID: "a/b"},
		"\x1b]7501;state=done:id=a/b:app=mytool\x1b\\")
	// Clear carries neither.
	want(ocs7501.Report{State: ocs7501.StateClear}, "\x1b]7501;state=clear\x1b\\")

	buf.Reset()
	if err := Clear("a"); err != nil || buf.String() != "\x1b]7501;state=clear:id=a\x1b\\" {
		t.Errorf("Clear: %q %v", buf.String(), err)
	}
	buf.Reset()
	if err := Query(); err != nil || buf.String() != "\x1b]7501;?\x1b\\\x1b[c" {
		t.Errorf("Query: %q %v", buf.String(), err)
	}

	// Removing the defaults.
	if err := SetDefaults("", ""); err != nil {
		t.Fatal(err)
	}
	want(ocs7501.Report{State: ocs7501.StateIdle}, "\x1b]7501;state=idle\x1b\\")
}

func TestSetDefaultsValidates(t *testing.T) {
	resetDefault(t)
	var buf bytes.Buffer
	SetOutput(&buf)
	if err := SetDefaults("good", "title"); err != nil {
		t.Fatal(err)
	}
	if err := SetDefaults("bad app", ""); !errors.Is(err, ocs7501.ErrApp) {
		t.Errorf("app: %v", err)
	}
	if err := SetDefaults("x", "a\nb"); !errors.Is(err, ocs7501.ErrText) {
		t.Errorf("title: %v", err)
	}
	// The failed calls changed nothing.
	_ = Send(ocs7501.Report{State: ocs7501.StateIdle})
	if !strings.Contains(buf.String(), "app=good") {
		t.Errorf("defaults lost: %q", buf.String())
	}
}

func TestDefaultDisabled(t *testing.T) {
	resetDefault(t)
	SetOutput(nil)
	if Enabled() {
		t.Error("enabled with nil output")
	}
	if err := Send(ocs7501.Report{State: ocs7501.StateIdle}); !errors.Is(err, ErrNoWriter) {
		t.Errorf("valid report: %v", err)
	}
	// Mistakes are still reported when nothing is attached.
	if err := Send(ocs7501.Report{State: ocs7501.StateIdle, Msg: "a\nb"}); !errors.Is(err, ocs7501.ErrText) {
		t.Errorf("invalid report: %v", err)
	}
	if err := Clear(""); !errors.Is(err, ErrNoWriter) {
		t.Errorf("clear: %v", err)
	}
}

func TestDefaultDetectsOnceLazily(t *testing.T) {
	resetDefault(t)
	def.mu.Lock()
	if def.s != nil {
		t.Fatal("detected before first use")
	}
	def.mu.Unlock()

	var first *Sender
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, _, _ := current()
			mu.Lock()
			defer mu.Unlock()
			if first == nil {
				first = s
			} else if s != first {
				t.Error("detection ran more than once")
			}
		}()
	}
	wg.Wait()
	if first == nil {
		t.Fatal("no sender")
	}
	// Under `go test` stdout is not a terminal; if it happens to be one the
	// detection picked a real stream, which is also fine.
	_ = Enabled()
}

func TestDefaultConcurrent(t *testing.T) {
	resetDefault(t)
	var mu sync.Mutex
	var n int
	SetOutput(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		n++
		mu.Unlock()
		return len(p), nil
	}))
	_ = SetDefaults("x", "y")
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = Send(ocs7501.Report{State: ocs7501.StateWorking})
				_ = SetDefaults("x", "y")
				Enabled()
			}
		}()
	}
	wg.Wait()
	if n != 800 {
		t.Errorf("%d writes", n)
	}
}

func TestTerminalOutNeverPanics(t *testing.T) {
	_ = terminalOut() // whatever the test runner's stdio is
}
