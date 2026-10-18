# ocs.7501 client

A terminal UI that sends [Program Status Protocol](https://www.superlogical.com/rex/docs/build/program-status)
(OSC 7501, spec revision 0.4) sequences to the terminal hosting it. Pick a command from the
menu, fill in its payload, check the live preview of the exact bytes, and send.

Besides OSC 7501 it can send the other inputs Claude Code (2.1.302) writes to its terminal:
title, bell and notifications, the startup queries, DEC mode switches, kitty keyboard and
modifyOtherKeys. See "Claude Code inputs" below and `claude-server.md`.

Built with [tview](https://github.com/rivo/tview); everything else is the standard library.
The UI uses a light pastel colour scheme. Every field comes pre-filled with a sensible default
(id `demo`, app `ocs-client`, title `OCS Client Demo`, a per-state message such as
"Task finished successfully", progress 40 for working / 60 for blocked, kind `permission`),
so **Enter sends immediately**; edit anything first.

## Build and run

```
cd D:\go\src\github.com\nontechno\ocs.7501\client
go mod tidy        # first time only: resolves deps and writes go.sum
go build -o ocs7501.exe .
.\ocs7501.exe
```

Run it directly inside the terminal you want to talk to (Rex, Windows Terminal, ...).
It writes to the console / controlling tty rather than stdout, so redirecting stdout
doesn't matter. For testing without a real terminal: `ocs7501 -out sequences.log`
appends the raw bytes to a file instead.

## Commands

| Menu entry            | Sends                                          | Payload fields                                     |
| --------------------- | ---------------------------------------------- | -------------------------------------------------- |
| idle                  | `OSC 7501 ; state=idle ... ST`                 | id, app, title, msg                                |
| working               | `state=working`                                | id, progress (0-100, empty = indeterminate), app, title, msg |
| done                  | `state=done`                                   | id, app, title, msg                                |
| blocked               | `state=blocked`                                | id, kind (permission/question/auth), progress, app, title, msg |
| error                 | `state=error`                                  | id, app, title, msg                                |
| clear                 | `state=clear`                                  | id (empty clears every record)                     |
| ? feature detection   | `OSC 7501 ; ? ST` (+ optional `CSI c`)         | none                                               |

Every command can be terminated with `ST` (`ESC \`) or `BEL`.

`title` and `msg` are typed as plain text and base64-encoded for you. The form checks the
spec's rules before it lets you send: single-line text (no control characters), 2048/192
decoded-byte caps for msg/title, app and id grammar, id depth, progress range, and the
4096-byte sequence cap. Keys the protocol says are ignored for a state (for example `kind`
outside `blocked`) are not shown and not sent.

## Claude Code inputs

All of these were captured from Claude Code itself (see `claude-server.md` for the bytes, their
source and what a terminal must do). Each has defaults, so Enter sends it immediately.

| Menu entry | Sends (default values) | Reply checked |
| --- | --- | --- |
| `title (OSC 0)` | `ESC ] 0 ; ✳ Claude Code BEL` | none |
| `bell (BEL)` | `BEL` | none |
| `notify · OSC 9` | `ESC ] 9 ; Claude needs your permission BEL` | none |
| `notify · OSC 99` | three chunks: `i=2325:d=0:p=title;Claude Code`, `i=2325:p=body;Claude needs your permission`, `i=2325:d=1:a=focus;` | none |
| `notify · OSC 777` | `ESC ] 777 ; notify ; Claude Code ; Claude needs your permission BEL` | none |
| `query · XTVERSION` | `ESC [ > 0 q` | `ESC P > \| text ESC \` |
| `query · kitty keyboard` | `ESC [ ? u` | `ESC [ ? flags u` |
| `query · device attributes` | `ESC [ c` | `ESC [ ? ... c` |
| `query · mode (DECRQM)` | `ESC [ ? 2026 $ p` (mode selectable) | `ESC [ ? mode ; 0-4 $ y` |
| `query · cell size` | `ESC [ 1 6 t` | `ESC [ 6 ; h ; w t` |
| `query · kitty graphics` | `ESC _ G i=31,s=1,v=1,a=q,t=d,f=24 ; AAAA ESC \` | `ESC _ G i=31 ; OK ESC \` (optional) |
| `query · startup burst` | the 12 startup queries, in order | all of the above, in order |
| `mode · set / reset` | `ESC [ ? 2004 h` (mode and action selectable) | none |
| `modes · Claude Code startup` | alternate screen, mouse, focus, paste, colour-scheme modes on | none |
| `modes · Claude Code exit` | the cleanup Claude Code writes on exit | none |
| `keyboard · kitty push/pop` | `ESC [ > 5 u` / `ESC [ < u` | none |
| `keyboard · modifyOtherKeys` | `ESC [ > 4 ; 2 m` / `ESC [ > 4 m` | none |

Terminator: the OSC entries default to `BEL` (what Claude Code uses); the OSC 7501 entries default
to `ST`. The dropdown overrides either.

`modes · Claude Code startup` and `mode · set / reset` change the terminal's state (the startup
entry switches to the alternate screen and clears it). Send `modes · Claude Code exit` to undo.

## Keys

| Key            | Action                          |
| -------------- | ------------------------------- |
| Up / Down      | choose a command                |
| Enter          | send the selected command (from the menu or any text field) |
| Tab / F2       | move from the menu to the payload form |
| F1             | back to the command menu        |
| F3             | focus the log, scroll with arrows / PgUp / PgDn |
| Tab / Shift-Tab| next / previous field           |
| Ctrl-S / F5    | send from anywhere (including dropdowns) |
| q / Ctrl-C     | quit                            |

Dropdowns, the checkbox and the buttons keep their own Enter behaviour (open / toggle / press).

## Reply check

Every command in the table above with a "Reply checked" entry (and `? feature detection`) listens for
1.5 s after sending; keys are ignored meanwhile. The reply arrives on stdin, so the app reads the
input bytes through a wrapper around tcell's tty and reports them exactly. The log then shows:

```
14:26:44 input during wait (not recognised as a reply): ? feature detection
  expected, and what was found:
    1. OSC 7501 feature-detection reply — ESC ] 7501 ; ? : states=<list> : kinds=<list> ESC \ (or ending in BEL)  ⇒ MISSING
    2. primary device attributes (DA1) reply — ESC [ ? <numbers separated by ;> c  ⇒ found: ESC [?62c
  received: 6 byte(s), exactly as read from the terminal
    text: ESC [?62c
    hex:  1b 5b 3f 36 32 63
  note: the terminal answered DA1 (ESC [ ? … c) but not the query above it, ...
```

Verdicts: `reply received`, `partial reply`, `input during wait (not recognised as a reply)` (input
arrived, the first expected reply is not in it) and `no reply seen`. If the wrapper cannot be installed
(`NewDevTty` fails) the bytes are rebuilt from key events, which is lossy; the report says so. Long
reports scroll: F3, then the arrow keys.

## Files

- `main.go` - the tview UI
- `inputs.go` - the Claude Code inputs: exact bytes, defaults and the expected-reply patterns
- `replycheck.go`, `tee.go` - reply matching/diagnosis and the raw input capture
- `inputs_test.go` - tests: observed byte strings, reply matching
- `claude-server.md` - how a terminal should receive and act on all of the above
- `protocol.go` - sequence building, base64 encoding, validation against the spec's limits (no UI dependencies)
- `protocol_test.go` - unit tests, using the examples from the spec as fixtures (`go test ./...`)
- `tty_unix.go`, `tty_windows.go` - opens `/dev/tty` or `CONOUT$` (enabling VT processing on Windows)
