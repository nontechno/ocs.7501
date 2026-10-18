//go:build windows

package main

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

const (
	enableProcessedOutput           = 0x0001
	enableVirtualTerminalProcessing = 0x0004
)

// openTTY returns a writer for the console's active screen buffer.
// CONOUT$ is used rather than stdout so the sequences reach the hosting
// terminal even if stdout is redirected. Virtual-terminal processing is
// switched on so the console (e.g. Windows Terminal / ConPTY) passes the
// escape sequence through instead of printing it.
func openTTY() (io.Writer, func(), string) {
	f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return os.Stdout, func() {}, "stdout"
	}
	k32 := syscall.NewLazyDLL("kernel32.dll")
	getMode := k32.NewProc("GetConsoleMode")
	setMode := k32.NewProc("SetConsoleMode")
	h := f.Fd()
	var mode uint32
	if r, _, _ := getMode.Call(h, uintptr(unsafe.Pointer(&mode))); r != 0 {
		setMode.Call(h, uintptr(mode|enableProcessedOutput|enableVirtualTerminalProcessing))
	}
	return f, func() { f.Close() }, "CONOUT$"
}
