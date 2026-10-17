package receiver_test

import (
	"fmt"

	"github.com/nontechno/ocs.7501/receiver"
)

// A terminal emulator with its own OSC parser hands over each OSC body.
func ExampleStore_Handle() {
	st := receiver.NewStore(0)

	st.Handle("7501;state=working:app=cargo")
	st.Handle("7501;state=done:id=build/test:title=VGVzdHM=")

	reply, _ := st.Handle("7501;?")
	fmt.Printf("%q\n", reply) // write this back to the program

	for _, r := range st.Records() {
		fmt.Printf("%q %s app=%s\n", r.ID, r.State, r.EffectiveApp)
	}
	// Output:
	// "\x1b]7501;?\x1b\\"
	// "" working app=cargo
	// "build/test" done app=cargo
}

// An observer that only sees the PTY output uses a Scanner.
func ExampleScanner() {
	var sc receiver.Scanner
	st := receiver.NewStore(0)

	// The stream may be cut anywhere.
	for _, chunk := range []string{"ls\r\n\x1b]7501;state=wor", "king:app=codex\x1b\\done\r\n"} {
		for _, m := range sc.Feed([]byte(chunk)) {
			if m.Report != nil {
				st.Apply(*m.Report)
			}
		}
	}
	r, _ := st.Get("")
	fmt.Println(r.State, r.App)
	// Output: working codex
}
