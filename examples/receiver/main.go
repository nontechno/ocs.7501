// Command receiver is an example terminal-side consumer. It reads a program's
// output from stdin (or a built-in sample with -demo), finds the OSC 7501
// status reports in it with receiver.Scanner, keeps them in a receiver.Store
// and prints what changed.
//
//	go run ./examples/sender | go run ./examples/receiver
//	go run ./examples/receiver -demo
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nontechno/ocs.7501/receiver"
)

// demo is what Codex writes (see task.md), mixed with ordinary output.
const demo = "hello\r\n" +
	"\x1b]7501;state=idle:app=codex\x1b\\" +
	"\x1b]7501;state=working:app=codex\x1b\\" +
	"\x1b[1mthinking...\x1b[0m\r\n" +
	"\x1b]7501;state=blocked:app=codex\x1b\\" +
	"\x1b]7501;state=working:app=codex\x1b\\" +
	"\x1b]7501;state=idle:app=codex\x1b\\" +
	"\x1b]7501;state=clear\x1b\\"

func main() {
	useDemo := flag.Bool("demo", false, "use a built-in sample stream instead of stdin")
	flag.Parse()

	var in io.Reader = os.Stdin
	if *useDemo {
		in = strings.NewReader(demo)
	}

	var sc receiver.Scanner
	store := receiver.NewStore(0)

	buf := make([]byte, 4096)
	for {
		n, err := in.Read(buf)
		for _, m := range sc.Feed(buf[:n]) {
			if m.Report == nil {
				continue // a feature query: a real terminal would reply here
			}
			if store.Apply(*m.Report) {
				show(store)
			}
		}
		if err != nil {
			break // io.EOF, or the pipe closed
		}
	}

	// The program is gone: working, blocked and idle records go; done and
	// error stay.
	if store.ProgramEnded() {
		fmt.Println("-- program ended")
		show(store)
	}
}

func show(s *receiver.Store) {
	recs := s.Records()
	if len(recs) == 0 {
		fmt.Println("(no records)")
		return
	}
	for _, r := range recs {
		id := r.ID
		if id == "" {
			id = "(root)"
		}
		prog := ""
		if r.Progress != nil {
			prog = fmt.Sprintf(" %d%%", *r.Progress)
		}
		kind := ""
		if r.Kind != "" {
			kind = " [" + string(r.Kind) + "]"
		}
		fmt.Printf("%-12s %-8s%s%s app=%s %q %q\n", id, r.State, kind, prog, r.EffectiveApp, r.Title, r.Msg)
	}
	fmt.Println()
}
