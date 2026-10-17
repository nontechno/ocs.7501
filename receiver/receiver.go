// Package receiver is the library for terminals (and for observers such as
// multiplexers, wrappers and test harnesses) that consume status reports sent
// by programs using the Program Status Protocol (OSC 7501).
//
// There are two pieces:
//
//   - Store keeps the current record per id and applies the protocol's rules
//     (full replacement, hierarchy, clearing, lifetime, limits, eviction).
//   - Scanner finds OSC 7501 sequences in a raw byte stream, for callers that
//     do not have their own escape-sequence parser.
//
// A terminal emulator that already parses OSC sequences hands the body of
// each OSC 7501 to Store.Handle and writes back the reply it returns. An
// observer that only sees the PTY output feeds the bytes to a Scanner and
// applies the messages to a Store:
//
//	var sc receiver.Scanner
//	st := receiver.NewStore(0)
//	for _, m := range sc.Feed(chunk) {
//		if m.Report != nil {
//			st.Apply(*m.Report)
//		}
//	}
//
// The library never writes to the program and never exposes ids, titles or
// messages for anything but display; what to display, and how to rate-limit
// notifications or sounds, is the caller's decision.
//
// All types are safe for concurrent use.
package receiver

import (
	"sync"

	ocs7501 "github.com/nontechno/ocs.7501"
	"github.com/nontechno/ocs.7501/internal/esc"
)

// Scanner extracts OSC 7501 messages from a byte stream (for example the
// output of a PTY). The stream may be split into chunks anywhere. Malformed
// or over-limit sequences are dropped silently, as the protocol says; all
// other bytes are ignored. The Scanner does not modify or filter the stream:
// the caller still passes it on.
//
// Only 7-bit escape sequences (ESC ]) are recognised. Because the Scanner
// does not model the whole terminal grammar (it knows nothing of DCS or APC
// strings that could contain an ESC ] pair), an emulator that has a real
// parser should prefer Store.Handle.
//
// The zero value is ready to use.
type Scanner struct {
	mu  sync.Mutex
	tok esc.Tokenizer
}

// Feed processes p and returns the messages completed by it, in order: status
// reports and feature queries ("?"). It returns nil if there are none.
func (s *Scanner) Feed(p []byte) []ocs7501.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ocs7501.Message
	s.tok.Feed(p, func(q esc.Seq) {
		if q.Kind != esc.OSC {
			return
		}
		if m, err := ocs7501.ParseBody(q.Body); err == nil {
			out = append(out, m)
		}
	})
	return out
}
