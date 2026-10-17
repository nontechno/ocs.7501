// Package ocs7501 implements the shared definitions of the Program Status
// Protocol (OSC 7501), version 0.4:
// https://www.superlogical.com/rex/docs/build/program-status
//
// A program reports whether it is idle, working, blocked on the user, done or
// failed by writing an escape sequence to its terminal:
//
//	ESC ] 7501 ; key=value [: key=value]... ST
//
// This package contains the pure, I/O-free part of the protocol: the Report
// type, the limits, and the encoder (Report.Encode, used by programs) and the
// decoder (ParseBody, used by terminals). It holds no state and is safe for
// concurrent use.
//
// Two small packages build on it:
//
//   - package sender, for programs that report their status;
//   - package receiver, for terminals (and observers such as multiplexers)
//     that consume those reports.
//
// Neither the encoder nor the decoder ever panics on any input; invalid input
// is reported as an error wrapping one of the Err* sentinels.
package ocs7501

// State is the value of the "state" key.
type State string

// The states defined by the protocol. StateClear is not a state: it removes
// the addressed record and all of its descendants.
const (
	StateIdle    State = "idle"
	StateWorking State = "working"
	StateDone    State = "done"
	StateBlocked State = "blocked"
	StateError   State = "error"
	StateClear   State = "clear"
)

// Kind is the value of the "kind" key; it only has meaning together with
// StateBlocked.
type Kind string

// The kinds defined by the protocol.
const (
	KindPermission Kind = "permission"
	KindQuestion   Kind = "question"
	KindAuth       Kind = "auth"
)

// Limits from the specification. Reports that exceed a limit are discarded
// whole.
const (
	MaxSequence     = 4096 // whole sequence, ESC ] ... ST, in bytes
	MaxKeyLen       = 16   // bytes
	MaxMsgEncoded   = 2732 // bytes of base64
	MaxMsgDecoded   = 2048 // bytes of UTF-8
	MaxTitleEncoded = 256  // bytes of base64
	MaxTitleDecoded = 192  // bytes of UTF-8
	MaxAppLen       = 32   // bytes
	MaxIDLen        = 128  // bytes, whole id
	MaxIDSegmentLen = 32   // bytes per segment
	MaxIDDepth      = 8    // segments
)

// Record-table sizes for terminals (see package receiver).
const (
	DefaultRecords = 256 // records a terminal keeps per default
	MinRecords     = 64  // the least a terminal may keep
)

// Wire constants.
const (
	// Number is the OSC parameter that identifies this protocol.
	Number = "7501"

	// QuerySequence is the feature query, OSC 7501 ; ? ST.
	QuerySequence = "\x1b]7501;?\x1b\\"

	// DeviceAttributes is the primary device attributes request (CSI c). A
	// program sends it right after QuerySequence: every terminal answers it,
	// so if its reply arrives first the protocol is not supported.
	DeviceAttributes = "\x1b[c"

	// DefaultReply is the feature-query reply of a terminal that accepts
	// exactly the default states and kinds.
	DefaultReply = "\x1b]7501;?\x1b\\"
)

// Report is one status report: the complete content of one record.
//
// Reports replace their record completely, so every report must carry every
// key it wants the record to have ("app" and "title" in particular).
type Report struct {
	// State is required.
	State State

	// ID addresses the record. Empty means the root record. Ids are paths of
	// segments of [A-Za-z0-9_.+-]{1,32} joined by "/", at most 8 deep and 128
	// bytes long; "build/test" is a child of "build".
	ID string

	// Kind says what a blocked program needs. Only valid with StateBlocked.
	// Empty means absent.
	Kind Kind

	// Progress is a percentage, 0-100. Only valid with StateWorking and
	// StateBlocked. Nil means indeterminate. See Percent.
	Progress *int

	// App is a stable program identifier ([A-Za-z0-9_.+-]{1,32}), such as
	// "cargo". Records without one inherit it from the nearest ancestor.
	App string

	// Title is a short label for the record (at most 192 bytes of UTF-8).
	// It must not contain control characters.
	Title string

	// Msg is one line describing the current state (at most 2048 bytes of
	// UTF-8). It must not contain control characters.
	Msg string
}

// Percent returns a pointer to n, for use as Report.Progress.
func Percent(n int) *int { return &n }

// Message is a decoded OSC 7501 sequence. Exactly one field is non-nil.
type Message struct {
	// Report is set for a status report.
	Report *Report

	// Features is set for a "?" sequence. Sent by a program it is a feature
	// query (and carries no features); sent by a terminal it is the reply to
	// one and lists what the terminal accepts. The two are the same bytes on
	// the wire when the terminal has no extras to list.
	Features *Features
}
