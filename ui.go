package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
)

type UI struct{}

func sid(s *Session) string {
	if len(s.ID) < 8 {
		return s.ID
	}
	return s.ID[:8]
}

func (UI) Header(provider, model string, s *Session) {
	turns := (len(s.Messages) - 1) / 2
	fmt.Printf("\n%s  %s · %s", dim, provider, model)
	if turns > 0 {
		fmt.Printf("  ·  %d turns", turns)
	}
	fmt.Printf("%s\n\n", reset)
}

func (UI) Prompt() { fmt.Printf("%s❯%s ", bold, reset) }

func (UI) AfterInput() { fmt.Println() }

func (UI) StartResponse() { fmt.Print("\n") }

func (UI) Token(t string) { fmt.Print(t) }

func (UI) Response() { fmt.Print("\n\n") }

func (UI) Tool(name string, input []byte) {
	fmt.Printf("%s  ⎿  %s%s %s%s\n", dim, bold, name, input, reset)
}

func (UI) ToolResult(result string) {
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	for _, l := range lines {
		fmt.Printf("%s     %s%s\n", dim, l, reset)
	}
}

func (UI) ToolError(err error) { fmt.Printf("%s  ✗  %s%s\n", red, err.Error(), reset) }

func (UI) Error(err error) { fmt.Printf("\n%s  ✗  %s%s\n\n", red, err.Error(), reset) }

func (UI) Info(msg string)    { fmt.Printf("%s  %s%s\n", dim, msg, reset) }
func (UI) Undone(path string) { fmt.Printf("%s  ↩  %s%s\n", yellow, path, reset) }

func (UI) SessionNew(id string) { fmt.Printf("\n%s  ○  new session%s\n\n", dim, reset) }

func (UI) SessionInfo(s *Session) {
	turns := (len(s.Messages) - 1) / 2
	fmt.Printf("\n%s  %s  %s  %d turns  %d edits%s\n\n",
		dim, sid(s), s.StartedAt.Format("Jan 2 15:04"), turns, len(s.Edits), reset)
}

func (UI) Spinner() func() {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	done, ack := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(ack)
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			case <-time.After(80 * time.Millisecond):
				select {
				case <-done:
					return
				default:
					fmt.Printf("\r\033[2K%s%s%s", dim, frames[i%len(frames)], reset)
					os.Stdout.Sync()
				}
			}
		}
	}()
	stopped := false
	return func() {
		if !stopped {
			stopped = true
			close(done)
			<-ack
			fmt.Printf("\r\033[2K")
		}
	}
}
