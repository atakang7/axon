package main

import (
	"bufio"
	"io"
	"time"
)

// pasteAwareInput returns an input function that coalesces lines arriving in
// rapid succession into a single message. The default behavior of bufio.Scanner
// is one line per call, which fragments multi-line pastes (e.g. a 20-line task
// prompt) into N separate user turns — the model then "answers" each line
// before the rest arrives, producing the appearance of an agent that can't
// read the prompt. We solve this by reading the first line blocking, then
// peeking with a short idle window: any further lines that arrive within
// pasteIdleWindow are treated as continuations of the same paste.
func pasteAwareInput(r io.Reader) func() (string, bool) {
	const pasteIdleWindow = 30 * time.Millisecond
	type lineMsg struct {
		line string
		ok   bool
	}
	lines := make(chan lineMsg)
	go func() {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			lines <- lineMsg{line: scanner.Text(), ok: true}
		}
		lines <- lineMsg{ok: false}
		close(lines)
	}()
	return func() (string, bool) {
		first, open := <-lines
		if !open || !first.ok {
			return "", false
		}
		buf := first.line
		for {
			select {
			case next, more := <-lines:
				if !more || !next.ok {
					return buf, true
				}
				buf += "\n" + next.line
			case <-time.After(pasteIdleWindow):
				return buf, true
			}
		}
	}
}

// singleShotInput returns the given prompt once, then EOF. Used by --prompt
// non-interactive mode.
func singleShotInput(prompt string) func() (string, bool) {
	delivered := false
	return func() (string, bool) {
		if delivered {
			return "", false
		}
		delivered = true
		return prompt, true
	}
}
