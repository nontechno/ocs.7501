// Package sender is the library for programs that report their status to the
// terminal they run in, using the Program Status Protocol (OSC 7501).
//
// Basic use:
//
//	s := sender.New(os.Stdout)
//	s.Send(ocs7501.Report{State: ocs7501.StateWorking, App: "mytool", Msg: "compiling"})
//	...
//	s.Send(ocs7501.Report{State: ocs7501.StateDone, App: "mytool", Msg: "built"})
//
// Every report replaces the previous one for the same id completely, so
// repeat App (and Title) in each report.
//
// Feature detection is optional: terminals ignore OSCs they do not know, so
// a program may simply send reports. To detect support, write Sender.Query,
// feed what the terminal sends back to a Detector, and stop waiting as soon
// as it decides. The library never reads from the terminal itself, so it
// cannot block on it.
//
// Sender and Detector are safe for concurrent use.
package sender

import (
	"errors"
	"io"
	"sync"

	ocs7501 "github.com/nontechno/ocs.7501"
)

// ErrNoWriter is returned by a Sender that has no writer.
var ErrNoWriter = errors.New("sender: no writer")

// Sender writes status reports to a terminal stream, typically os.Stdout or
// os.Stderr when it is a terminal, or the controlling tty.
//
// Each report is written with one Write call while a lock is held, so
// reports sent from several goroutines never interleave with one another.
// The Sender cannot keep other writers (for example a buffered logger on the
// same stream) from splitting a sequence; send everything through one
// writer, or flush before sending.
//
// Writes go straight to the underlying writer and take as long as it takes:
// a terminal that stops reading will eventually block the write. A Sender
// does not add buffering or goroutines of its own.
type Sender struct {
	mu sync.Mutex
	w  io.Writer
}

// New returns a Sender that writes to w. A nil w yields a Sender whose
// methods return ErrNoWriter.
func New(w io.Writer) *Sender { return &Sender{w: w} }

// Send encodes r and writes it. Nothing is written if r is not a legal
// report; the returned error then wraps one of the ocs7501.Err* values.
func (s *Sender) Send(r ocs7501.Report) error {
	seq, err := r.Encode()
	if err != nil {
		return err
	}
	return s.write(seq)
}

// Clear removes the record with the given id and all of its descendants;
// with an empty id it removes all of this terminal's records. Programs
// should do this on exit for records they will not finish (done and error
// records are meant to outlive the program).
func (s *Sender) Clear(id string) error {
	return s.Send(ocs7501.Report{State: ocs7501.StateClear, ID: id})
}

// Query writes the feature query (OSC 7501 ; ? ST) followed by a primary
// device attributes request (CSI c). Feed the terminal's answer to a
// Detector.
func (s *Sender) Query() error {
	return s.write(ocs7501.QuerySequence + ocs7501.DeviceAttributes)
}

func (s *Sender) write(seq string) error {
	if s == nil {
		return ErrNoWriter
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return ErrNoWriter
	}
	n, err := io.WriteString(s.w, seq)
	if err == nil && n < len(seq) {
		err = io.ErrShortWrite
	}
	return err
}
