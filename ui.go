//go:build !api

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Color palette modeled on Claude Code's CLI: orange-tinted brand accent,
// muted grays for secondary content, semantic colors for status. Uses
// 256-color ANSI for portability.
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	italic = "\033[3m"

	// Brand: warm orange/amber, used for the prompt sigil and section markers.
	brand = "\033[38;5;215m"

	// Content tones.
	fg     = "\033[38;5;253m" // assistant content (bright)
	mute   = "\033[38;5;243m" // dim secondary lines
	hint   = "\033[38;5;238m" // very dim — backgrounds, separators
	think  = "\033[38;5;245m" // reasoning tokens (subordinate to content)
	toolFg = "\033[38;5;110m" // tool name (cyan-ish)
	argsFg = "\033[38;5;176m" // tool args (lavender)
	pathFg = "\033[38;5;117m" // file paths inside args

	// Code block background tint (faint grey, like Claude Code).
	codeBg = "\033[48;5;235m\033[38;5;253m"

	// Semantic.
	ok   = "\033[38;5;78m"  // green
	warn = "\033[38;5;221m" // yellow
	bad  = "\033[38;5;203m" // red
)

// uiState tracks streaming state across token callbacks: are we inside a
// fenced code block, was the last emitted token reasoning vs content, etc.
// All UI write paths funnel through here so reasoning/content/tool-arg
// streams interleave cleanly.
type uiState struct {
	mu          sync.Mutex
	inFence     bool   // currently inside a ``` block
	fenceBuf    string // partial backtick run while detecting fences
	lastKind    byte   // 'c' content, 'r' reasoning, 't' tool-arg, 0 none
	atLineStart bool
}

var ui = &uiState{atLineStart: true}

// uiSilent suppresses all UI output. Set in non-interactive mode where the
// JSONL event log is the source of truth and TUI rendering would just
// pollute logs.
var uiSilent bool

func uiHeader(provider, model string, s *Session) {
	if uiSilent {
		return
	}
	fmt.Printf("\n%s%s axon%s  %s%s · %s%s",
		bold, brand, reset,
		mute, provider, model, reset)
	if s.Turn > 0 {
		fmt.Printf("%s  ·  %d turns%s", mute, s.Turn, reset)
	}
	fmt.Printf("\n\n")
}

func uiPrompt() {
	if uiSilent {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	fmt.Printf("%s%s❯%s ", bold, brand, reset)
	ui.atLineStart = false
	ui.lastKind = 0
}

func uiAfterInput() { fmt.Println() }

func uiStartResponse() {
	if uiSilent {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	if !ui.atLineStart {
		fmt.Println()
	}
	ui.atLineStart = true
}

// uiToken streams an assistant content token. Detects ``` fences and applies
// a faint background tint inside code blocks for visual grouping.
func uiToken(t string) {
	if uiSilent {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	if ui.lastKind != 0 && ui.lastKind != 'c' {
		fmt.Print(reset)
		if !ui.atLineStart {
			fmt.Println()
		}
	}
	ui.lastKind = 'c'
	for _, r := range t {
		ch := string(r)
		// fence detection: accumulate runs of backticks
		if r == '`' {
			ui.fenceBuf += ch
			if ui.fenceBuf == "```" {
				ui.inFence = !ui.inFence
				if ui.inFence {
					fmt.Print(reset + codeBg + "```")
				} else {
					fmt.Print("```" + reset)
				}
				ui.fenceBuf = ""
				continue
			}
			continue
		}
		if ui.fenceBuf != "" {
			// flush any incomplete fence prefix
			if ui.inFence {
				fmt.Print(codeBg + ui.fenceBuf + reset)
			} else {
				fmt.Print(fg + ui.fenceBuf + reset)
			}
			ui.fenceBuf = ""
		}
		if ui.inFence {
			fmt.Print(codeBg + ch + reset)
		} else {
			fmt.Print(fg + ch + reset)
		}
		ui.atLineStart = (r == '\n')
	}
	os.Stdout.Sync()
}

// uiReasoning streams a reasoning/thinking token. Rendered as a side-channel:
// dim grey with a left margin character so it's clearly subordinate.
func uiReasoning(t string) {
	if uiSilent {
		return
	}
	if t == "" {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	if ui.lastKind != 'r' {
		fmt.Print(reset)
		if !ui.atLineStart {
			fmt.Println()
		}
		fmt.Printf("%s%s╎ %s", think, italic, reset)
		ui.atLineStart = false
	}
	ui.lastKind = 'r'
	for _, r := range t {
		ch := string(r)
		if r == '\n' {
			fmt.Print(reset)
			fmt.Println()
			fmt.Printf("%s%s╎ %s", think, italic, reset)
			ui.atLineStart = false
			continue
		}
		fmt.Printf("%s%s%s%s", think, italic, ch, reset)
	}
	os.Stdout.Sync()
}

// uiToolArgDelta streams partial tool-call argument JSON as it arrives. The
// full pretty-printed args are re-rendered by uiTool when the call resolves;
// this is the live "typing" view.
func uiToolArgDelta(name, delta string) {
	if uiSilent {
		return
	}
	if delta == "" {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	if ui.lastKind != 't' {
		fmt.Print(reset)
		if !ui.atLineStart {
			fmt.Println()
		}
		fmt.Printf("%s  ⎯ %s%s%s ", mute, toolFg, name, reset)
		ui.atLineStart = false
	}
	ui.lastKind = 't'
	fmt.Printf("%s%s%s", argsFg, delta, reset)
	os.Stdout.Sync()
}

func uiResponse() {
	if uiSilent {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	fmt.Print(reset)
	if !ui.atLineStart {
		fmt.Println()
	}
	fmt.Println()
	ui.atLineStart = true
	ui.lastKind = 0
}

// uiTool renders a finalized tool call: name on one line, pretty-printed
// args indented below. Replaces the streaming arg-delta view.
func uiTool(name string, input []byte) {
	if uiSilent {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	if ui.lastKind == 't' {
		// terminate the streaming-args line cleanly
		fmt.Print(reset)
		if !ui.atLineStart {
			fmt.Println()
		}
	}
	ui.lastKind = 0
	ui.atLineStart = true
	fmt.Printf("\n%s  ⎿  %s%s%s%s\n", mute, bold, toolFg, name, reset)
	pretty := prettyArgs(input)
	for _, line := range strings.Split(pretty, "\n") {
		if line == "" {
			continue
		}
		fmt.Printf("%s     %s%s%s\n", hint, argsFg, line, reset)
	}
}

func uiToolResult(r string) {
	if uiSilent {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.lastKind = 0
	ui.atLineStart = true
	lines := strings.Split(strings.TrimSpace(r), "\n")
	if len(lines) > 8 {
		hidden := len(lines) - 8
		fmt.Printf("%s     │ %s[%d earlier lines hidden]%s\n", hint, mute, hidden, reset)
		lines = lines[len(lines)-8:]
	}
	for _, l := range lines {
		fmt.Printf("%s     │ %s%s%s\n", hint, mute, l, reset)
	}
}

func uiToolError(err error) {
	if uiSilent {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.lastKind = 0
	ui.atLineStart = true
	fmt.Printf("%s  ✗  %s%s\n", bad, err.Error(), reset)
}

func uiError(err error) {
	if uiSilent {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.lastKind = 0
	ui.atLineStart = true
	fmt.Printf("\n%s  ✗  %s%s\n\n", bad, err.Error(), reset)
}

func uiInfo(m string) {
	if uiSilent {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.lastKind = 0
	ui.atLineStart = true
	fmt.Printf("%s  %s%s\n", mute, m, reset)
}

func uiUndone(p string) {
	if uiSilent {
		return
	}
	fmt.Printf("%s  ↩  %s%s\n", warn, p, reset)
}

func uiMemory(m string) {
	if uiSilent {
		return
	}
	fmt.Printf("%s  ↺  %s%s\n", warn, m, reset)
}

func uiSessionNew() {
	if uiSilent {
		return
	}
	fmt.Printf("\n%s  ○  new session%s\n\n", mute, reset)
}

func uiSessionInfo(s *Session) {
	if uiSilent {
		return
	}
	id := s.ID
	if len(id) > 8 {
		id = id[:8]
	}
	fmt.Printf("\n%s  %s%s%s  %s  %d turns  %d edits%s\n\n",
		mute, bold, id, reset+mute, s.StartedAt.Format("Jan 2 15:04"),
		s.Turn, len(s.Edits), reset)
}

func uiSpinner() func() {
	if uiSilent {
		return func() {}
	}
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
					fmt.Printf("\r\033[2K%s%s%s%s", brand, frames[i%len(frames)], reset, "")
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

// prettyArgs reformats a tool-call JSON argument blob for human reading.
// Falls back to raw on parse failure.
func prettyArgs(raw []byte) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}
