package sender

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	ocs7501 "github.com/nontechno/ocs.7501"
)

func TestSend(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf)
	if err := s.Send(ocs7501.Report{State: ocs7501.StateWorking, App: "codex"}); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), "\x1b]7501;state=working:app=codex\x1b\\"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	buf.Reset()
	if err := s.Clear(""); err != nil || buf.String() != "\x1b]7501;state=clear\x1b\\" {
		t.Errorf("clear: %q %v", buf.String(), err)
	}
	buf.Reset()
	if err := s.Clear("a/b"); err != nil || buf.String() != "\x1b]7501;state=clear:id=a/b\x1b\\" {
		t.Errorf("clear id: %q %v", buf.String(), err)
	}
	buf.Reset()
	if err := s.Query(); err != nil || buf.String() != "\x1b]7501;?\x1b\\\x1b[c" {
		t.Errorf("query: %q %v", buf.String(), err)
	}
}

func TestSendInvalidWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf)
	err := s.Send(ocs7501.Report{State: ocs7501.StateIdle, Msg: "a\nb"})
	if !errors.Is(err, ocs7501.ErrText) || buf.Len() != 0 {
		t.Errorf("%v, %q", err, buf.String())
	}
}

type failWriter struct{ short bool }

func (f failWriter) Write(p []byte) (int, error) {
	if f.short {
		return len(p) - 1, nil
	}
	return 0, errors.New("boom")
}

func TestWriteErrors(t *testing.T) {
	r := ocs7501.Report{State: ocs7501.StateIdle}
	if err := New(failWriter{}).Send(r); err == nil || err.Error() != "boom" {
		t.Errorf("%v", err)
	}
	if err := New(failWriter{short: true}).Send(r); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("%v", err)
	}
	if err := New(nil).Send(r); !errors.Is(err, ErrNoWriter) {
		t.Errorf("%v", err)
	}
	var nilSender *Sender
	if err := nilSender.Send(r); !errors.Is(err, ErrNoWriter) {
		t.Errorf("%v", err)
	}
	if err := nilSender.Query(); !errors.Is(err, ErrNoWriter) {
		t.Errorf("%v", err)
	}
}

func TestConcurrentSendsDoNotInterleave(t *testing.T) {
	var mu sync.Mutex
	var chunks []string
	w := writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		chunks = append(chunks, string(p))
		mu.Unlock()
		return len(p), nil
	})
	s := New(w)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = s.Send(ocs7501.Report{State: ocs7501.StateWorking, ID: fmt.Sprintf("g%d", g), Msg: "m"})
			}
		}(g)
	}
	wg.Wait()
	if len(chunks) != 1600 {
		t.Fatalf("%d writes", len(chunks))
	}
	for _, c := range chunks {
		if !strings.HasPrefix(c, "\x1b]7501;") || !strings.HasSuffix(c, "\x1b\\") || strings.Count(c, "\x1b") != 2 {
			t.Fatalf("not exactly one sequence per write: %q", c)
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func feedAll(d *Detector, data string, chunk int) Result {
	var r Result
	for i := 0; i < len(data); i += chunk {
		end := i + chunk
		if end > len(data) {
			end = len(data)
		}
		r = d.Feed([]byte(data[i:end]))
	}
	return r
}

func TestDetector(t *testing.T) {
	cases := []struct {
		name, in string
		want     Result
	}{
		{"supported, default", "\x1b]7501;?\x1b\\\x1b[?62;22c", Supported},
		{"supported, BEL", "\x1b]7501;?:states=idle,working\x07\x1b[?1;2c", Supported},
		{"unsupported", "\x1b[?62;22c", Unsupported},
		{"noise before", "hello\x1b[A\x1b]0;x\x07\x1b[?6c", Unsupported},
		{"first wins (reply then DA)", "\x1b]7501;?\x1b\\\x1b[?6c", Supported},
		{"first wins (DA then reply)", "\x1b[?6c\x1b]7501;?\x1b\\", Unsupported},
		{"secondary DA is not ours", "\x1b[>1;2;3c", Pending},
		{"a report is not a reply", "\x1b]7501;state=idle\x1b\\", Pending},
		{"nothing", "", Pending},
	}
	for _, c := range cases {
		for _, chunk := range []int{1, 3, 1 << 20} {
			var d Detector
			if got := feedAll(&d, c.in, chunk); got != c.want {
				t.Errorf("%s (chunk %d): got %v want %v", c.name, chunk, got, c.want)
			}
		}
	}
}

func TestDetectorFeatures(t *testing.T) {
	var d Detector
	d.Feed([]byte("\x1b]7501;?:states=idle,done:kinds=auth:future=1\x1b\\"))
	res, f := d.Result()
	if res != Supported || !f.SupportsState(ocs7501.StateIdle) || f.SupportsState(ocs7501.StateWorking) ||
		!f.SupportsKind(ocs7501.KindAuth) || f.SupportsKind(ocs7501.KindQuestion) {
		t.Errorf("%v %+v", res, f)
	}
	// Decided results are sticky.
	if d.Feed([]byte("\x1b[?6c")) != Supported {
		t.Error("result changed")
	}
	if Pending.String() != "pending" || Supported.String() != "supported" || Unsupported.String() != "unsupported" {
		t.Error("String")
	}
}

func FuzzDetector(f *testing.F) {
	f.Add([]byte("\x1b]7501;?\x1b\\"), []byte("\x1b[?6c"))
	f.Fuzz(func(t *testing.T, a, b []byte) {
		var d Detector
		d.Feed(a)
		d.Feed(b)
		d.Result()
	})
}
