package sender_test

import (
	"bytes"
	"fmt"

	ocs7501 "github.com/nontechno/ocs.7501"
	"github.com/nontechno/ocs.7501/sender"
)

func Example() {
	var out bytes.Buffer // in a real program: os.Stdout, when it is a terminal
	s := sender.New(&out)

	// Each report replaces the record completely: repeat App and Title.
	s.Send(ocs7501.Report{State: ocs7501.StateWorking, App: "mytool", Title: "Build", Progress: ocs7501.Percent(10)})
	s.Send(ocs7501.Report{State: ocs7501.StateDone, App: "mytool", Title: "Build", Msg: "ok"})

	fmt.Printf("%q\n", out.String())
	// Output: "\x1b]7501;state=working:progress=10:app=mytool:title=QnVpbGQ=\x1b\\\x1b]7501;state=done:app=mytool:title=QnVpbGQ=:msg=b2s=\x1b\\"
}

func ExampleDetector() {
	// After Sender.Query, the program reads the terminal's answer from its
	// input and feeds it to a Detector. Here the answer is a fixed string.
	var d sender.Detector
	res := d.Feed([]byte("\x1b]7501;?:states=idle,working,done\x1b\\\x1b[?62;c"))
	_, features := d.Result()
	fmt.Println(res, features.SupportsState(ocs7501.StateBlocked))
	// Output: supported false
}
