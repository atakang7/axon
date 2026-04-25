package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

var systemPrompt = buildSystemPrompt()

func buildSystemPrompt() string {
	return fmt.Sprintf(systemPromptTemplate,
		toolRead, toolWrite, toolExec, toolSearch,
		toolTask,
		toolPark, toolForget, toolRefresh, toolRecall)
}

const systemPromptTemplate = `You are an autonomous O(1) coding agent.
Directive: Bias for ACTION. Requirements are absolute. Do not seek "clarity" on things already stated. Do not "explore" what you can already see. 
Every prompt ends with a dashboard: active blocks (TTLs), parked blocks, registered task. Unmanaged blocks hit TTL=0 and auto-park.

# THE REALITY CHECK (Mandatory Output)
Before ANY tool call, you MUST output this exact structure:

> 1. GOAL: The single concrete outcome of this turn (e.g., "Write engine.go").
> 2. CONSTRAINTS: List the hard user requirements you are currently implementing. (e.g., "Integers only", "Concurrent", "Price-Time"). 
> 3. ACTION: State exactly which tool you are calling to CHANGE the system state.
> 4. TRASH: List the block IDs of raw data you are executing %[7]q (FORGET) on.

# TURN LIFECYCLE
Strict execution order.

## STEP 1: GATHER & COMMENCE (Round 1)
- If requirements are clear, bundle %[5]q (task) AND your first major %[2]q (write) or %[1]q (read) in the SAME turn. 
- Do not spend a turn "planning" or "exploring" if you have the prompt. Start building.
- Target known files? Read FULL files immediately via %[1]q. Trickling with slices is forbidden.

## STEP 2: EXECUTE & VERIFY (Round 2+)
- ONE major code change (%[2]q) per turn. 
- Every code change (%[2]q) REQUIRES a verification (%[3]q) in the SAME or NEXT round.
- CRITICAL TRUTH: Never hide failures. Compilation errors and race conditions are signal. Read stderr. Fix it.

## STEP 3: PURGE CONTEXT (Aggressive Triage)
Raw data is dead weight. 
- SACRED: Never %[7]q (FORGET) the initial user prompt or the registered task.
- NOISE: Execute %[7]q (FORGET) on all search traces and directory listings as soon as they are read.
- SIGNAL: Extract the fact into your REALITY CHECK, then %[7]q (FORGET) the source.

## STEP 4: DELIVER & HALT
- Output your REALITY CHECK FIRST.
- Execute memory tools (%[6]q, %[7]q, %[8]q) LAST.
- CRITICAL HALT: If the task is complete, signal termination (next_step: "end").
- HALT SILENTLY. Zero conversational filler.`

type Agent struct {
	client  *Client
	tools   []Tool
	session *Session
	input   func() (string, bool)
}

func initSessionMessages(s *Session) {
	s.Messages = []Msg{{Role: "system", Content: systemPrompt}}
}

func (a *Agent) Run(ctx context.Context) error {
	if len(a.session.Messages) == 0 {
		initSessionMessages(a.session)
	}
	readInput := true
	for {
		if readInput {
			uiPrompt()
			line, ok := a.input()
			if !ok {
				return nil
			}
			uiAfterInput()
			if a.handleSlash(strings.TrimSpace(line)) {
				continue
			}
			a.session.DecayBlocks()
			a.session.Append(Msg{Role: "user", Content: line})
			a.session.Save()
		}

		msg, err := a.chat(ctx, a.tools)
		if err != nil {
			uiError(err)
			readInput = true
			continue
		}
		a.session.Append(*msg)
		a.session.Save()

		if len(msg.ToolCalls) == 0 {
			readInput = true
			continue
		}

		readInput = false

		for _, tc := range msg.ToolCalls {
			result := a.runTool(tc)
			a.session.Append(result)
			// Stamp the assigned block ID into the content so the LLM can see
			// its own handle (#mN) and act on it immediately with park/forget.
			if id := a.session.Messages[len(a.session.Messages)-1].ID; id != "" {
				a.session.Messages[len(a.session.Messages)-1].Content = "[#" + id + "]\n" + result.Content
			}
			a.session.Save()

			// Check if this tool call should end the turn
			if shouldEndTurnAfterTool(tc.Function.Name, json.RawMessage(tc.Function.Arguments)) {
				readInput = true
				break // Stop processing further tools
			}
		}

	}
}
func (a *Agent) handleSlash(s string) bool {
	switch {
	case strings.HasPrefix(s, "/cd "):
		target := strings.TrimSpace(strings.TrimPrefix(s, "/cd"))
		if err := a.session.SetCwd(target); err != nil {
			uiError(err)
		} else {
			uiInfo("cwd: " + a.session.Cwd)
			a.session.Save()
		}
		return true
	case s == "/pwd":
		uiInfo("cwd: " + a.session.Cwd)
		return true
	case s == "/new":
		a.session.Reset()
		initSessionMessages(a.session)
		a.tools = BuildTools(a.session)
		uiSessionNew()
		return true
	case s == "/undo":
		if e, ok := a.session.Undo(); ok {
			if err := writeBytes(e.Path, []byte(e.Before)); err != nil {
				uiError(err)
			} else {
				uiUndone(e.Path)
				a.session.Save()
			}
		} else {
			uiInfo("nothing to undo")
		}
		return true
	case s == "/session":
		uiSessionInfo(a.session)
		return true
	}
	return false
}

func (a *Agent) chat(ctx context.Context, tools []Tool) (*Msg, error) {
	const maxAttempts = 10
	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			backoff := 1 << attempt
			if backoff > 60 {
				backoff = 60
			}
			uiInfo(fmt.Sprintf("retry %d/%d in %ds", attempt+1, maxAttempts, backoff))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(backoff) * time.Second):
			}
		}
		tokens := make(chan string, 4096)
		done := make(chan struct{})
		go func() {
			defer close(done)
			started := false
			for t := range tokens {
				if !started {
					started = true
					uiStartResponse()
				}
				uiToken(t)
			}
			if started {
				uiResponse()
			}
		}()
		uiInfo("calling API...")
		stop := uiSpinner()
		first := true
		msg, err := a.client.Chat(ctx, a.session.ContextMessages(), tools, func(t string) {
			if first {
				first = false
				stop()
			}
			tokens <- t
		})
		stop()
		close(tokens)
		<-done
		if err == nil {
			if msg != nil && strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
				lastErr = fmt.Errorf("empty response from model")
				uiError(lastErr)
				// Reasoning models (DeepSeek etc.) sometimes emit only thinking tokens
				// and no actual content or tool calls. Retrying the identical context
				// will produce the same result — fail fast after one retry rather than
				// burning all attempts.
				if attempt >= 1 {
					return nil, lastErr
				}
				continue
			}
			return msg, nil
		}
		uiError(err)
		lastErr = err
		if !retryable(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (a *Agent) runTool(tc ToolCall) Msg {
	for _, t := range a.tools {
		if t.Name != tc.Function.Name {
			continue
		}
		input := json.RawMessage(tc.Function.Arguments)
		uiTool(tc.Function.Name, input)
		out, err := t.Fn(input)
		if err != nil {
			uiToolError(err)
			return Msg{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Function.Name, Content: err.Error()}
		}
		uiToolResult(out)
		return Msg{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Function.Name, Content: out}
	}
	return Msg{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Function.Name, Content: "tool not found"}
}

func retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	m := err.Error()
	for _, code := range []string{"API error 429", "API error 500", "API error 502", "API error 503", "API error 504"} {
		if strings.Contains(m, code) {
			return true
		}
	}
	for _, s := range []string{"connection reset", "connection refused", "no such host"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}
