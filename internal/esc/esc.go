// Package esc is a minimal, allocation-light tokenizer that finds OSC 7501
// sequences (and, optionally, CSI sequences) in a byte stream. It is the
// shared engine of receiver.Scanner and sender.Detector.
//
// It understands only the 7-bit forms (ESC ], ESC [, ST = ESC \ or BEL).
// Everything else in the stream is ignored. CAN and SUB cancel a sequence in
// progress, and an ESC that does not complete a terminator aborts it and
// starts a new one, as in a real terminal.
//
// A Tokenizer is not safe for concurrent use; its owners lock around it. The
// zero value is ready to use.
package esc

// Kind says what a Seq is.
type Kind uint8

const (
	// OSC is an OSC sequence whose body starts with "7501;".
	OSC Kind = iota + 1
	// CSI is a CSI sequence.
	CSI
)

// Seq is one recognised sequence.
type Seq struct {
	Kind Kind
	// Body is, for OSC, the bytes between "ESC ]" and the terminator; for
	// CSI, the parameter and intermediate bytes between "ESC [" and Final.
	Body string
	// Final is the final byte of a CSI sequence.
	Final byte
}

const (
	maxSequence = 4096 // whole OSC, including ESC ] and terminator
	maxCSI      = 64   // bytes of CSI parameters we are willing to keep
	oscPrefix   = "7501;"
)

type state uint8

const (
	ground state = iota
	sawEsc
	inOSC    // collecting a 7501 OSC body
	inOSCEsc // ESC seen inside inOSC
	skipOSC  // an OSC we do not want (or cannot want); wait for its end
	skipEsc  // ESC seen inside skipOSC
	inCSI    // collecting CSI bytes
	skipCSI  // CSI we do not collect; wait for the final byte
)

// Tokenizer scans a byte stream.
type Tokenizer struct {
	// WantCSI makes Feed report CSI sequences too. When false they are
	// skipped without being stored.
	WantCSI bool

	st  state
	buf []byte
}

// Feed processes p and calls emit for each complete sequence found, in
// order. State is kept between calls, so p may be split anywhere.
func (t *Tokenizer) Feed(p []byte, emit func(Seq)) {
	for i := 0; i < len(p); i++ {
		b := p[i]
		switch t.st {
		case ground:
			if b == 0x1b {
				t.st = sawEsc
			}

		case sawEsc:
			switch b {
			case ']':
				t.st, t.buf = inOSC, t.buf[:0]
			case '[':
				if t.WantCSI {
					t.st, t.buf = inCSI, t.buf[:0]
				} else {
					t.st = skipCSI
				}
			case 0x1b:
				// still an ESC
			default:
				t.st = ground
			}

		case inOSC:
			switch {
			case b == 0x07:
				t.finishOSC(1, emit)
			case b == 0x1b:
				t.st = inOSCEsc
			case b == 0x18 || b == 0x1a:
				t.reset()
			default:
				t.buf = append(t.buf, b)
				if !prefixOK(t.buf) || len(t.buf) > maxSequence-3 {
					t.st, t.buf = skipOSC, t.buf[:0]
				}
			}

		case inOSCEsc:
			if b == '\\' {
				t.finishOSC(2, emit)
			} else {
				t.st = sawEsc
				i-- // look at b again as the byte after a fresh ESC
			}

		case skipOSC:
			switch b {
			case 0x07, 0x18, 0x1a:
				t.reset()
			case 0x1b:
				t.st = skipEsc
			}

		case skipEsc:
			if b == '\\' {
				t.reset()
			} else {
				t.st = sawEsc
				i--
			}

		case inCSI:
			switch {
			case b >= 0x40 && b <= 0x7e:
				s := Seq{Kind: CSI, Body: string(t.buf), Final: b}
				t.reset()
				emit(s)
			case b >= 0x20 && b <= 0x3f:
				t.buf = append(t.buf, b)
				if len(t.buf) > maxCSI {
					t.st, t.buf = skipCSI, t.buf[:0]
				}
			case b == 0x1b:
				t.st = sawEsc
			case b == 0x18 || b == 0x1a || b >= 0x7f:
				t.reset()
			}

		case skipCSI:
			switch {
			case b >= 0x40 && b <= 0x7e, b == 0x18, b == 0x1a:
				t.reset()
			case b == 0x1b:
				t.st = sawEsc
			}

		default:
			t.reset()
		}
	}
}

func (t *Tokenizer) reset() {
	t.st, t.buf = ground, t.buf[:0]
}

// finishOSC ends a 7501 OSC whose terminator was termLen bytes long.
func (t *Tokenizer) finishOSC(termLen int, emit func(Seq)) {
	var s Seq
	ok := len(t.buf) >= len(oscPrefix) && 2+len(t.buf)+termLen <= maxSequence
	if ok {
		s = Seq{Kind: OSC, Body: string(t.buf)}
	}
	t.reset()
	if ok {
		emit(s)
	}
}

// prefixOK reports whether buf can still become a body starting "7501;".
func prefixOK(buf []byte) bool {
	n := len(buf)
	if n > len(oscPrefix) {
		n = len(oscPrefix)
	}
	return string(buf[:n]) == oscPrefix[:n]
}
