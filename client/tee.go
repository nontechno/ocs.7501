package main

import (
	"sync"

	"github.com/gdamore/tcell/v2"
)

// teeTTY wraps tcell's tty and records every byte read from the terminal
// while a capture is active, so a reply can be reported exactly as it
// arrived instead of as key events decoded by tcell.
type teeTTY struct {
	tcell.Tty
	mu  sync.Mutex
	on  bool
	buf []byte
}

func (t *teeTTY) Read(p []byte) (int, error) {
	n, err := t.Tty.Read(p)
	if n > 0 {
		t.mu.Lock()
		if t.on {
			t.buf = append(t.buf, p[:n]...)
		}
		t.mu.Unlock()
	}
	return n, err
}

// begin starts a fresh capture.
func (t *teeTTY) begin() {
	t.mu.Lock()
	t.on, t.buf = true, nil
	t.mu.Unlock()
}

// end stops the capture and returns what was read.
func (t *teeTTY) end() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.on = false
	b := t.buf
	t.buf = nil
	return b
}

// installTee gives the application a screen that reads through a teeTTY. If
// anything fails the default screen is used and replies are reconstructed
// from key events instead.
func (u *ui) installTee() {
	tty, err := tcell.NewDevTty()
	if err != nil {
		return
	}
	tee := &teeTTY{Tty: tty}
	scr, err := tcell.NewTerminfoScreenFromTty(tee)
	if err != nil {
		tty.Close()
		return
	}
	u.app.SetScreen(scr)
	u.tee = tee
}
