package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	reset = "\033[0m"
	bold  = "\033[1m"
	dim   = "\033[2m"
	yel   = "\033[33m"
	red   = "\033[31m"
)

func uiHeader(provider, model string, s *Session) {
	fmt.Printf("\n%s  %s · %s", dim, provider, model)
	if s.Turn > 0 {
		fmt.Printf("  ·  %d turns", s.Turn)
	}
	fmt.Printf("%s\n\n", reset)
}

func uiPrompt()        { fmt.Printf("%s❯%s ", bold, reset) }
func uiAfterInput()    { fmt.Println() }
func uiStartResponse() { fmt.Print("\n") }
func uiToken(t string) { fmt.Print(t) }
func uiResponse()      { fmt.Print("\n\n") }

func uiTool(name string, input []byte) {
	fmt.Printf("%s  ⎿  %s%s %s%s\n", dim, bold, name, input, reset)
}

func uiToolResult(r string) {
	lines := strings.Split(strings.TrimSpace(r), "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	for _, l := range lines {
		fmt.Printf("%s     %s%s\n", dim, l, reset)
	}
}

func uiToolError(err error) { fmt.Printf("%s  ✗  %s%s\n", red, err.Error(), reset) }
func uiError(err error)     { fmt.Printf("\n%s  ✗  %s%s\n\n", red, err.Error(), reset) }
func uiInfo(m string)       { fmt.Printf("%s  %s%s\n", dim, m, reset) }
func uiUndone(p string)     { fmt.Printf("%s  ↩  %s%s\n", yel, p, reset) }
func uiMemory(m string)     { fmt.Printf("%s  ↺  %s%s\n", yel, m, reset) }
func uiSessionNew()         { fmt.Printf("\n%s  ○  new session%s\n\n", dim, reset) }

func uiSessionInfo(s *Session) {
	id := s.ID
	if len(id) > 8 {
		id = id[:8]
	}
	fmt.Printf("\n%s  %s  %s  %d turns  %d edits%s\n\n", dim, id, s.StartedAt.Format("Jan 2 15:04"), s.Turn, len(s.Edits), reset)
}

func uiSpinner() func() {
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
