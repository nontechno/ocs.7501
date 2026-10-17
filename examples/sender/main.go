// Command sender is an example program that reports its status with
// package sender. Run it in a terminal that supports OSC 7501, or pipe it to
// the receiver example (the example always writes to standard output):
//
//	go run ./examples/sender | go run ./examples/receiver
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	ocs7501 "github.com/nontechno/ocs.7501"
	"github.com/nontechno/ocs.7501/sender"
)

func main() {
	delay := flag.Duration("delay", 0, "pause between steps, e.g. 500ms")
	flag.Parse()

	// Once, at start-up. The terminal itself is found on the first Send.
	if err := sender.SetDefaults("demo", "Demo upgrade"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// This example is meant to be piped into examples/receiver, so send to
	// standard output even though it is not a terminal then.
	sender.SetOutput(os.Stdout)

	send := func(r ocs7501.Report) {
		// A status line is never worth failing the real work: ignore errors.
		_ = sender.Send(r)
		time.Sleep(*delay)
	}

	send(ocs7501.Report{State: ocs7501.StateIdle, Msg: "ready"})
	for p := 0; p <= 60; p += 20 {
		send(ocs7501.Report{State: ocs7501.StateWorking, Progress: ocs7501.Percent(p), Msg: "downloading"})
	}
	send(ocs7501.Report{State: ocs7501.StateBlocked, Kind: ocs7501.KindAuth, Msg: "password needed"})
	send(ocs7501.Report{State: ocs7501.StateWorking, Msg: "installing"}) // no progress: indeterminate

	// A child record, addressed by id: "demo/tests" is a child of the root.
	send(ocs7501.Report{State: ocs7501.StateDone, ID: "demo/tests", Title: "Tests", Msg: "all passed"})

	// done survives the program's exit; the terminal keeps it until the user
	// has seen it. So do not Clear on the way out after a normal finish. A
	// program that exits without finishing (for example after Ctrl-C) should
	// report idle or call sender.Clear("") instead.
	send(ocs7501.Report{State: ocs7501.StateDone, Msg: "finished"})
}
