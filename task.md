# Info:

```txt
Codex writes these semantic status subsequences to the PTY output stream:
Idle:
	\x1b]7501;state=idle:app=codex\x1b\\
Working:
	\x1b]7501;state=working:app=codex\x1b\\

Waiting for user:
	\x1b]7501;state=blocked:app=codex\x1b\\
Exit cleanup:
	\x1b]7501;state=clear\x1b\\

Here \x1b] is OSC and \x1b\\ is the string terminator. 
This is OSC 7501, the Program Status Protocol 
(https://www.superlogical.com/rex/docs/build/program-status), 
whose grammar is:
OSC 7501 ; key=value (: key=value)* ST
```

# Goal

1. load and comprehend https://www.superlogical.com/rex/docs/build/program-status
2. create two golang-based libraries: 
	- one to be used by terminal (to receive info/msgs from apps)
	- another to be used by apps (to send info/msgs to terminal)
 using the Program Status Protocol
3. the libraries should be correct, safe (no panics, blocks, locks, multithread save, etc), simple (do not add anything that is not in the spec)
4. provided examples of use for each libraty
5. provided sufficent documentation of how to use, etc
6. save the results under D:\go\src\github.com\nontechno\ocs.7501\
