package sender

import (
	"io"
	"os"
	"sync"

	ocs7501 "github.com/nontechno/ocs.7501"
)

// The process-wide default sender behind the package-level functions Send,
// Clear, Query, SetDefaults, SetOutput and Enabled. It is created on first
// use, under a lock, exactly once (until SetOutput replaces it).
var def struct {
	mu    sync.Mutex
	s     *Sender
	app   string
	title string
}

// terminalOut picks the stream that status reports should go to: standard
// output if it is a terminal, else standard error if it is, else nil. A
// program whose output is piped or redirected therefore sends nothing.
func terminalOut() io.Writer {
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		if f == nil {
			continue
		}
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return f
		}
	}
	return nil
}

// current returns the default sender, creating it on first use, together
// with the default app and title.
func current() (s *Sender, app, title string) {
	def.mu.Lock()
	defer def.mu.Unlock()
	if def.s == nil {
		def.s = New(terminalOut())
	}
	return def.s, def.app, def.title
}

// Enabled reports whether the package-level functions have a terminal to
// write to. The first call performs the detection.
func Enabled() bool {
	s, _, _ := current()
	return s.hasWriter()
}

// SetDefaults sets the App and Title that the package-level Send adds to
// every report that does not carry its own. Call it once at start-up. Every
// report replaces its record completely, so a program that always wants its
// name and label on screen should not have to repeat them.
//
// The default Title applies only to root records (reports without an ID):
// a child record has a title of its own. The default App applies to all
// reports, and is harmless on children, which inherit it anyway. Clear
// reports carry neither.
//
// Empty strings remove a default. It returns an error, and changes nothing,
// if either value would make reports illegal (see ocs7501.Report).
func SetDefaults(app, title string) error {
	if err := (ocs7501.Report{State: ocs7501.StateIdle, App: app, Title: title}).Validate(); err != nil {
		return err
	}
	def.mu.Lock()
	def.app, def.title = app, title
	def.mu.Unlock()
	return nil
}

// SetOutput replaces the default sender's destination, overriding the
// terminal detection. A nil w disables the package-level functions. It is
// meant for tests and for programs that open the controlling terminal
// themselves.
func SetOutput(w io.Writer) {
	def.mu.Lock()
	def.s = New(w)
	def.mu.Unlock()
}

// Send sends a report through the default sender, filling in the defaults
// from SetDefaults. The report is validated first, so mistakes show up even
// when no terminal is attached.
//
// When there is no terminal (output is piped or redirected) nothing is
// written and ErrNoWriter is returned. That is not a failure: programs
// should normally ignore the error, since a status report is never worth
// failing the real work:
//
//	_ = sender.Send(ocs7501.Report{State: ocs7501.StateWorking, Msg: "compiling"})
func Send(r ocs7501.Report) error {
	s, app, title := current()
	if r.State != ocs7501.StateClear {
		if r.App == "" {
			r.App = app
		}
		if r.Title == "" && r.ID == "" {
			r.Title = title
		}
	}
	return s.Send(r)
}

// Clear sends a clear for id through the default sender; see Sender.Clear.
func Clear(id string) error {
	s, _, _ := current()
	return s.Clear(id)
}

// Query writes the feature query through the default sender; see
// Sender.Query.
func Query() error {
	s, _, _ := current()
	return s.Query()
}
