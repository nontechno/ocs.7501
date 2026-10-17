// Command sender is an example program that reports its status with
// package sender. Run it in a terminal that supports OSC 7501, or pipe it to
// the receiver example:
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

	s := sender.New(os.Stdout)
	send := func(r ocs7501.Report) {
		// Every report replaces the previous one, so each repeats App and Title.
		r.App, r.Title = "demo", "Demo upgrade"
		if err := s.Send(r); err != nil {
			fmt.Fprintln(os.Stderr, "send:", err)
		}
		time.Sleep(*delay)
	}

	send(ocs7501.Report{State: ocs7501.StateIdle, Msg: "ready"})
	for p := 0; p <= 60; p += 20 {
		send(ocs7501.Report{State: ocs7501.StateWorking, Progress: ocs7501.Percent(p), Msg: "downloading"})
	}
	send(ocs7501.Report{State: ocs7501.StateBlocked, Kind: ocs7501.KindAuth, Msg: "password needed"})
	send(ocs7501.Report{State: ocs7501.StateWorking, Msg: "installing"}) // no progress: indeterminate

	// A child record, addressed by id: "demo/tests" is a child of "demo".
	s.Send(ocs7501.Report{State: ocs7501.StateDone, ID: "demo/tests", Title: "Tests", Msg: "all passed"})

	// done survives the program's exit; the terminal keeps it until the user
	// has seen it. So do not Clear on the way out after a normal finish. A
	// program that exits without finishing (for example after Ctrl-C) should
	// report idle or call s.Clear("") instead.
	send(ocs7501.Report{State: ocs7501.StateDone, Msg: "finished"})
}
