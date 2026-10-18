//go:build !windows

package main

import (
	"io"
	"os"
)

// openTTY returns a writer for the controlling terminal. Writing to
// /dev/tty (rather than stdout) means the sequences reach the hosting
// terminal even if stdout is redirected.
func openTTY() (io.Writer, func(), string) {
	if f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		return f, func() { f.Close() }, "/dev/tty"
	}
	return os.Stdout, func() {}, "stdout"
}
