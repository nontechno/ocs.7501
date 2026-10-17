# ocs.7501 — Program Status Protocol (OSC 7501) for Go

Go libraries for the [Program Status Protocol](https://www.superlogical.com/rex/docs/build/program-status)
(spec version 0.4), the escape sequence a program uses to tell its terminal
whether it is idle, working, blocked on the user, done or failed:

```
ESC ] 7501 ; key=value [: key=value]... ST          ST is ESC \ (or BEL)
```

Example, as written by Codex:

```
\x1b]7501;state=idle:app=codex\x1b\\        idle
\x1b]7501;state=working:app=codex\x1b\\     working
\x1b]7501;state=blocked:app=codex\x1b\\     waiting for the user
\x1b]7501;state=clear\x1b\\                 exit cleanup
```

Standard library only, Go 1.21+.

| Package | For | Main API |
|---|---|---|
| `github.com/nontechno/ocs.7501` (name `ocs7501`) | both sides | `Report`, `Report.Encode`, `ParseBody`, limits, `Features` |
| `.../sender` | **apps** that send status | `sender.New(w)`, `Send`, `Clear`, `Query`, `Detector` |
| `.../receiver` | **terminals** that receive status | `receiver.Store` (`Handle`, `Apply`, `Get`, `Records`, `ProgramEnded`, `Reset`), `receiver.Scanner` |

```go
import (
    ocs7501 "github.com/nontechno/ocs.7501"
    "github.com/nontechno/ocs.7501/sender"
    "github.com/nontechno/ocs.7501/receiver"
)
```

## For apps: package `sender`

```go
s := sender.New(os.Stdout)   // any io.Writer connected to the terminal

s.Send(ocs7501.Report{State: ocs7501.StateWorking, App: "mytool", Title: "Build",
    Progress: ocs7501.Percent(40), Msg: "compiling"})

s.Send(ocs7501.Report{State: ocs7501.StateBlocked, Kind: ocs7501.KindPermission,
    App: "mytool", Title: "Build", Msg: "Allow network access?"})

s.Send(ocs7501.Report{State: ocs7501.StateDone, App: "mytool", Title: "Build", Msg: "ok"})
```

Rules worth knowing (all from the spec):

* **A report replaces its record completely.** Anything you leave out is gone,
  so repeat `App` and `Title` in every report.
* `ID` addresses a record; empty means the root. `"build/test"` is a child of
  `"build"`. Children inherit `App` from the nearest ancestor that has one.
* `Kind` only with `StateBlocked`; `Progress` (0–100, nil = indeterminate) only
  with `StateWorking` and `StateBlocked`. `Title` and `Msg` are UTF-8 without
  control characters (no newlines or tabs). `Encode`/`Send` return an error
  and write **nothing** if a report breaks a rule or a limit.
* `done` and `error` outlive the program and the next prompt, so a program that
  finishes should report `done` or `error` and *not* `Clear`. A program that is
  interrupted should report `idle` (or `Clear`). `working` and `blocked` are
  dropped by the terminal when the process exits.
* No heartbeat is needed; send a report only when something changes.
* `s.Clear(id)` removes a record and its descendants; `s.Clear("")` removes all
  of the terminal's records.

### Feature detection (optional)

Terminals ignore unknown OSCs, so you can just send. To find out whether the
terminal supports the protocol:

1. `s.Query()` — writes `OSC 7501 ; ? ST` followed by `CSI c` (device attributes).
2. Read the terminal's answer from stdin (raw mode is your business) and give the bytes to a
   `sender.Detector`:

```go
var d sender.Detector
s.Query()
for d.Feed(readFromTerminal()) == sender.Pending { }
res, features := d.Result()          // sender.Supported / sender.Unsupported
if res == sender.Supported && features.SupportsState(ocs7501.StateBlocked) { ... }
```

Every terminal answers `CSI c`; if that answer arrives first, the protocol is
unsupported. The library never reads from the terminal itself, so it cannot
block waiting for one; add your own timeout if you want a safety net.

## For terminals: package `receiver`

**If you have your own escape-sequence parser** (a terminal emulator), pass the
body of every `OSC 7501` to a `Store`:

```go
store := receiver.NewStore(0)                    // 0 = 256 records (minimum 64)

// parser callback: body is what lies between "ESC ]" and the terminator
reply, changed := store.Handle(body)             // "7501;state=working:app=cargo"
if reply != "" { pty.Write([]byte(reply)) }      // answer to a "?" query
if changed     { redraw(store.Records()) }
```

**If you only see the PTY output** (multiplexer, wrapper, test harness), use a
`Scanner`, which accepts the stream in chunks of any size:

```go
var sc receiver.Scanner
for _, m := range sc.Feed(chunk) {
    if m.Report != nil { store.Apply(*m.Report) }
}
```

Also on the terminal's side:

* `store.ProgramEnded()` — call when the attached process exits and when the
  shell starts a prompt (OSC 133 `A`). Drops `working`, `blocked` and `idle`
  records, keeps `done` and `error`.
* `store.Reset()` — call on a full reset (`RIS`). Do **not** call it on a soft
  reset (`DECSTR`). Switching screens does nothing.
* `store.Get(id)`, `store.Records()` — copies of the records, ordered by id.
  `Record.EffectiveApp` is the app after inheritance.
* When the store is full, the least recently updated record is evicted.
* Presentation, desktop notifications and sounds (which the spec says to
  rate-limit) are yours to decide; the library only reports `changed`.
  Show `Title`/`Msg` as plain text, and say which terminal a record came from.

What the receiving side enforces (`ocs7501.ParseBody`):

* malformed pairs are skipped and the rest of the report is processed; unknown
  keys are ignored; the last of repeated keys wins; whitespace around keys and
  values is trimmed;
* unknown/missing `state`, or an invalid `id`, makes the report ignored (an
  invalid id never falls back to the root);
* invalid `kind`, `progress` or `app` are treated as absent;
* a report is **discarded whole** if it exceeds a limit (4096 bytes in total,
  16-byte keys, `msg` 2732/2048, `title` 256/192, ids 128/32/8 levels), if its
  base64 is invalid, or if decoded `title`/`msg` is not UTF-8 or contains a
  control character (C0, DEL, C1);
* everything is checked before the store is touched;
* invisible formatting characters (bidirectional overrides, zero-width and tag
  characters, U+2028/2029) are removed from `title`/`msg`; the joiners U+200C/D
  are kept because scripts and emoji need them.

## Safety

* **No panics.** Every entry point validates its input and returns an error
  (wrapping an `ocs7501.Err*` value) or silently drops it; fuzz tests
  (`FuzzParseBody`, `FuzzEncode`, `FuzzScanner`, `FuzzDetector`) cover this.
* **No unbounded memory.** The scanner keeps at most 4 KiB per sequence and
  skips anything that cannot be a valid report; the store is capped.
* **No goroutines, no callbacks under locks, no reads.** The libraries never
  start goroutines and never block on anything but the `io.Writer` you give
  `sender.New` (a terminal that stops reading will eventually block a write
  to it; the library adds no buffering of its own).
* **Concurrency.** `Sender`, `Detector`, `Scanner` and `Store` are safe for use
  from multiple goroutines (tests run with `-race`). A `Sender` writes each
  report with a single `Write` under a lock, so reports never interleave with
  one another. It cannot stop *other* writers on the same stream from
  splitting a sequence, so send everything through one writer.
* The terminal writes back exactly one thing: the feature-query reply.

## Examples

```
go run ./examples/sender | go run ./examples/receiver     # whole pipeline
go run ./examples/receiver -demo                          # the Codex sequences
```

Runnable godoc examples are in each package (`example_test.go`).

## Tests

```
go test -race ./...
go test -run xxx -fuzz FuzzParseBody -fuzztime 30s .
```

## Scope and decisions

The libraries implement what the spec defines and nothing else. Where the spec
leaves room:

* `Report.Encode` is stricter than the receiver: it refuses to emit `kind`
  outside `blocked`, `progress` outside `working`/`blocked`, unknown states and
  kinds, and `clear` with anything but an id, instead of emitting something the
  terminal would have to ignore.
* Base64 is emitted with padding; it is accepted with or without.
* Sequences are emitted with the `ESC \` terminator and both `ESC \` and `BEL`
  are accepted. Only the 7-bit introducer `ESC ]` is recognised.
* `Store.ProgramEnded` also drops `idle` records (the spec says it *may*).
* The spec text was read from the page as of 2026-10-17 (revision 0.4); a
  later revision may change details.

The wire format is stable, but the Go API is `v0`.
