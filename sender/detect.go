package sender

import (
	"strings"
	"sync"

	ocs7501 "github.com/nontechno/ocs.7501"
	"github.com/nontechno/ocs.7501/internal/esc"
)

// Result is the outcome of feature detection.
type Result int

const (
	// Pending means neither answer has been seen yet.
	Pending Result = iota
	// Supported means the terminal answered the feature query.
	Supported
	// Unsupported means the device attributes reply arrived first, so the
	// terminal ignored the feature query.
	Unsupported
)

func (r Result) String() string {
	switch r {
	case Supported:
		return "supported"
	case Unsupported:
		return "unsupported"
	}
	return "pending"
}

// Detector interprets the terminal's answer to Sender.Query. The terminal
// answers a supported query with OSC 7501 ; ? [: states=... : kinds=...] ST;
// it answers the device attributes request that follows (CSI ? ... c) in
// every case. Whichever arrives first decides.
//
// The program reads its terminal input (in raw mode, which is its own
// business) and passes the bytes to Feed until the result is no longer
// Pending. Because a terminal always answers the device attributes request,
// the wait ends; a program that wants a safety net anyway can use its own
// timeout. Unrelated bytes are ignored.
//
// The zero value is ready to use. Safe for concurrent use.
type Detector struct {
	mu       sync.Mutex
	tok      esc.Tokenizer
	result   Result
	features ocs7501.Features
}

// Feed processes bytes read from the terminal and returns the current
// result. Once decided, the result never changes.
func (d *Detector) Feed(p []byte) Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.result != Pending {
		return d.result
	}
	d.tok.WantCSI = true
	d.tok.Feed(p, func(s esc.Seq) {
		if d.result != Pending {
			return
		}
		switch s.Kind {
		case esc.OSC:
			if m, err := ocs7501.ParseBody(s.Body); err == nil && m.Features != nil {
				d.result, d.features = Supported, *m.Features
			}
		case esc.CSI:
			if s.Final == 'c' && strings.HasPrefix(s.Body, "?") {
				d.result = Unsupported
			}
		}
	})
	return d.result
}

// Result returns the current result and, when it is Supported, the
// terminal's features. Reply pairs the library does not understand are
// ignored, as the protocol requires.
func (d *Detector) Result() (Result, ocs7501.Features) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.result, d.features
}
