package ocs7501_test

import (
	"fmt"

	ocs7501 "github.com/nontechno/ocs.7501"
)

func ExampleReport_Encode() {
	seq, err := ocs7501.Report{
		State:    ocs7501.StateBlocked,
		Kind:     ocs7501.KindPermission,
		Progress: ocs7501.Percent(80),
		App:      "mytool",
		Msg:      "Allow network access?",
	}.Encode()
	fmt.Printf("%q %v\n", seq, err)
	// Output: "\x1b]7501;state=blocked:kind=permission:progress=80:app=mytool:msg=QWxsb3cgbmV0d29yayBhY2Nlc3M/\x1b\\" <nil>
}

func ExampleParseBody() {
	// The body of an OSC sequence: what lies between ESC ] and the terminator.
	m, err := ocs7501.ParseBody("7501;state=working:app=codex:progress=50")
	fmt.Println(err, m.Report.State, m.Report.App, *m.Report.Progress)

	// Invalid reports are rejected; the error says why.
	_, err = ocs7501.ParseBody("7501;state=busy")
	fmt.Println(err)
	// Output:
	// <nil> working codex 50
	// ocs7501: state "busy": missing or unknown state
}
