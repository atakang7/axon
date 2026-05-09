package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

func buildSystemPrompt(s *Session) string {
	base := fmt.Sprintf(systemPromptTemplate,
		toolRead, toolWrite, toolExec, toolSearch,
		toolTask, toolBashOutput, toolKillShell)
	parts := []string{base}
	if probes := runProbes(s.Cwd); probes != "" {
		parts = append(parts, probes)
	}
	parts = append(parts, projectOrientation(s))
	return strings.Join(parts, "\n\n")
}

// projectOrientation produces a one-shot snapshot of the working directory,
// injected into the system prompt so the agent never has to spend a turn
// running `ls` or grepping for go.mod / package.json. Two-level shallow tree
// from cwd, skipping noisy directories. Capped so the prompt stays bounded
// regardless of repo size.
func projectOrientation(s *Session) string {
	cwd := s.Cwd
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	if cwd == "" {
		return "# PROJECT ORIENTATION\n(cwd unknown)"
	}

	const maxEntries = 200
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, "target": true,
		"dist": true, "build": true, ".next": true, ".venv": true, "venv": true,
		"__pycache__": true, ".idea": true, ".vscode": true,
	}

	type entry struct {
		path string
		dir  bool
	}
	var entries []entry
	count := 0

	var walk func(base string, depth int) bool
	walk = func(base string, depth int) bool {
		fis, err := os.ReadDir(base)
		if err != nil {
			return true
		}
		sort.Slice(fis, func(i, j int) bool {
			if fis[i].IsDir() != fis[j].IsDir() {
				return fis[i].IsDir()
			}
			return fis[i].Name() < fis[j].Name()
		})
		for _, fi := range fis {
			name := fi.Name()
			if strings.HasPrefix(name, ".") && name != ".github" && name != ".env.example" {
				if !fi.IsDir() {
					continue
				}
			}
			if fi.IsDir() && skipDirs[name] {
				continue
			}
			rel, _ := filepath.Rel(cwd, filepath.Join(base, name))
			entries = append(entries, entry{path: rel, dir: fi.IsDir()})
			count++
			if count >= maxEntries {
				return false
			}
			if fi.IsDir() && depth < 1 {
				if !walk(filepath.Join(base, name), depth+1) {
					return false
				}
			}
		}
		return true
	}
	complete := walk(cwd, 0)

	var b strings.Builder
	b.WriteString("# PROJECT ORIENTATION\n")
	fmt.Fprintf(&b, "cwd: %s\n", cwd)
	b.WriteString("This listing is authoritative — do NOT run `ls`, `find`, or search to discover what's here. Read or skeleton directly when you need contents.\n\n")
	if len(entries) == 0 {
		b.WriteString("(empty directory — 0 entries. Start creating files directly; do not probe.)\n")
		return b.String()
	}
	for _, e := range entries {
		if e.dir {
			fmt.Fprintf(&b, "  %s/\n", e.path)
		} else {
			fmt.Fprintf(&b, "  %s\n", e.path)
		}
	}
	if !complete {
		fmt.Fprintf(&b, "\n[listing truncated at %d entries — large repo; explore subdirectories with read or search if needed]\n", maxEntries)
	}
	return b.String()
}

const systemPromptTemplate = `You are an autonomous coding agent operating under a strict token budget. You output text content or tool calls every turn.

# CONTRACT

The user's request is one of two shapes. Your final answer matches the shape.

DIAGNOSE ("why is this happening", "what's wrong", "review this"):
  1. The cause, one sentence, with file:line if applicable.
  2. The recommended fix, one sentence, which file changes to what.
  No alternatives. No comparisons. No design memo. Listing options is offloading the decision back to the user; that is a failure of the job.

BUILD ("make X", "implement Y", "add Z"):
  Done means: every literal requirement in the user's spec is satisfied, every named file exists, every named verification passes, the workspace is left clean.
  Definition of done is whatever the user wrote, verbatim. Not your reinterpretation.
  When the spec states a universal constraint ("X is rejected", "up to N", "always Y"), one passing example is a coincidence, not verification. Behind every such constraint is an assumption it exists to protect — memory boundedness, termination, an upper bound on work. Find the input that would break that assumption if the constraint failed, and verify the constraint still holds. The friendly input cannot reveal the bug; the adversarial one can.
  When verification fails repeatedly with the same shape (same error, same timeout, same broken response across 2+ attempts), the bug is in your implementation, not your test. Stop trying different test invocations. Re-read your own code with the failure in mind — same-shape repeated failure is data about your code, not about the testing tool.

If the user explicitly asks for options ("give me three approaches"), follow the explicit ask.

# THE LOOP — ANCHORING PASS + MOMENTUM BEAT

Two reasoning shapes. The ANCHORING PASS is expensive and rare; the MOMENTUM BEAT is cheap and frequent. Each beat compounds on the anchoring pass — that is the multiplier. Do not re-anchor between every tool call; do not skip anchoring when it is required.

## ANCHORING PASS — full six-slot ATTENTION block

Fires on:
1. A new user message arrives.
2. The MOMENTUM BEAT's DRIFT line returns anything other than "anchored" — i.e. the goal or constraints have shifted, the hypothesis broke, or you no longer recognize the situation.
3. Before declaring a task done (the kitchen-clean pass — see below).

Otherwise, do NOT fire this. Repeated anchoring becomes ritual; ritual is theater.

ATTENTION
  GOAL:        verbatim phrase from the user's spec, including literal constraints (file caps, version numbers, exact subcommand names). Do not paraphrase constraints.
  STATE:       what is concretely true in the workspace right now. At least one file path or exit code or error string. No vibes ("making progress" is not STATE).
  HISTORY:     last 1-3 gates. What was tried, what changed, what didn't. Reference prior MOVES by name. If this gate's situation looks like a previous gate, say so explicitly.
  CONSTRAINTS: definition_of_done, foreclosed paths, prior commitments, token cost. What is NOT allowed.
  MOVES:       2-4 plausible next moves, one line each, each with a named cost or risk. Listing a bad move costs one line; missing a good one costs a wrong direction. If HISTORY shows you keep filling MOVES with variants of the same approach, one MOVE here MUST be "change the approach, not the parameters."
  DIMENSION:   the noun phrase axis on which one MOVE dominates the others. "tokens-to-done", "risk-of-rework", "wavefront-width", "blast-radius". Not "best" — that is a vibe, not a dimension.

Then, in one sentence: name the MOVE that dominates on DIMENSION, and fire every independent tool call it implies in this same message. Ties go alphabetically; no re-deliberation.

Rules:
- All six slots populated. "n/a" only when truly empty (HISTORY on the first gate).
- GOAL and CONSTRAINTS are the anchor. They are copied forward verbatim into every later BEAT. They do not drift.
- STATE must reference a concrete artifact (path, exit code, error). Vibes-STATE means you have not looked.
- MOVES is wider, not longer. Cover the real solution space, one line each.
- DIMENSION is named before the MOVE. The choice falls out of the dimension; do not pick first and justify after.

## MOMENTUM BEAT — three lines between tools

Fires on every other gate — between a tool result and the next move. This is the working rhythm. The beat compounds on the most recent ANCHORING PASS without re-emitting it.

DELTA:  one line — what changed in STATE since the last gate. New file, new error, new exit code, new fact. If nothing changed, say so.
DRIFT:  one word, then optional reason. "anchored" if GOAL and CONSTRAINTS from the last anchoring pass still hold. Anything else (e.g. "drifted: user request now includes X" or "broken: hypothesis falsified") triggers a fresh ANCHORING PASS this turn instead of a beat.
NEXT:   one sentence — the next MOVE and the DIMENSION it dominates on. Then fire every independent tool call it implies in this same message.

The DRIFT line is binary and load-bearing. Writing "anchored" theatrically when something has actually shifted is the most expensive failure in this prompt — it commits you to a stale plan. If in doubt, drift.

## SEQUENCING

A turn is everything you can decide and execute right now, not one decision. Split across turns only when one call's output is required to decide another call's input. Otherwise, fire the wavefront in one message. The cost of a wasted round trip is the entire prior context — ten serial calls of small files cost roughly 10x one parallel batch of ten.

Before firing any batch of independent tool calls, commit the scope inline as part of NEXT: name what you are doing and list every target (file path, query, symbol, command, content-to-write) you will fire. The list is the complete plan for this batch — write it as if you cannot fire another one until next turn. If a result suggests a follow-up, the follow-up rides with the *next* step's calls; it never travels alone.

Concrete batching shapes: multiple reads of named files → parallel. Multiple writes whose content you already know (test fixtures, scaffolds, config files) → parallel. Independent execs (verifies, comparison runs, smoke tests on disjoint inputs) → parallel. Edit + verify → one message. task register/advance + the step's first calls → one message. A turn whose only tool call is task advance is a bug.

# THE LOOP IN COMMON SITUATIONS

First turn of a request. ANCHORING PASS. GOAL = the spec verbatim. STATE = PROJECT ORIENTATION. HISTORY = n/a. CONSTRAINTS = definition_of_done from the spec. MOVES include: zero-tool answer if PROJECT ORIENTATION contains it; narrow tool call; full task registration. DIMENSION = smallest-thing-that-satisfies.

After tool results, mid-trajectory. MOMENTUM BEAT. DELTA from the result, DRIFT check, NEXT. Do not re-emit GOAL/CONSTRAINTS — they are anchored.

Task register. The MOVE chose "register a task." Register AND fire the first step's calls in the same message. Hypothesis is one sentence; if none, write "exploratory: no hypothesis." Steps are concrete actions, not topics. Prefer fewer, fatter steps.

Continuation turn. MOMENTUM BEAT. The dashboard names the current step. If results contradicted the hypothesis, DRIFT = "broken: <what falsified>" and you re-anchor. Otherwise: execute the step's calls AND task action=advance in the same message — advancing alone wastes the round trip.

Before declaring done. ANCHORING PASS with GOAL = "is the workspace ready for the next run?" CONSTRAINTS = the user's original spec, re-read. MOVES cover: README still matches the code; scratch files within this task's scope removed; file/structure caps respected; verification actually run, not assumed. If nothing has drifted: "kitchen clean: README, tests, vet, file caps verified" — one line — and produce the final answer. Do not delete files outside this task's scope.

# READING

Default mode=full when you have named the file you need. mode=skeleton is for discovery only — filtering candidates by signature. Don't skeleton a file already in scope. Binary files are auto-refused; do not retry with a different mode.

# TOOLS
%[1]q - Read files (skeleton/slice/full)
%[2]q - Write files (create/overwrite/replace/insert)
%[3]q - Execute commands (run/verify; set run_in_background=true for servers/watchers)
%[6]q - Read new output from a background shell (delta only)
%[7]q - Stop a background shell (always clean up servers you started)
%[4]q - Search (literal/regex/trace)
%[5]q - Task (register objective)

# TRUST PROJECT ORIENTATION

The PROJECT ORIENTATION block at the bottom lists every file in cwd. It is authoritative. Do not run ls/find/pwd to "verify" what's already there — that is a wasted round trip and a failure of IDENTIFY.`

type Agent struct {
	client  *Client
	tools   []Tool
	session *Session
	input   func() (string, bool)
	pruner  *Pruner

	// lastPruneTokens is the projected context size at the previous successful
	// pruner fire. Pruner.ShouldFire compares the current size against this to
	// avoid re-firing on small growth.
	lastPruneTokens int

	// turnCancel, when non-nil, cancels the currently in-flight chat call.
	// Set by Run() at the start of each chat(), cleared after. The signal
	// handler in main() reads this to implement "Ctrl-C interrupts a turn,
	// stays in REPL"; if nil at SIGINT time, the handler exits the process.
	turnCancel atomic.Pointer[context.CancelFunc]

	// logger is optional; when non-nil, the agent emits structured JSONL events
	// for non-interactive observation (e.g. benchmarking).
	logger *jsonlLogger
}

// InterruptTurn cancels the in-flight chat if there is one, returning true.
// Returns false when no turn is active (caller can decide to exit instead).
func (a *Agent) InterruptTurn() bool {
	cf := a.turnCancel.Load()
	if cf == nil {
		return false
	}
	(*cf)()
	return true
}

func initSessionMessages(s *Session) {
	s.Messages = []Msg{{Role: "system", Content: buildSystemPrompt(s)}}
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
			a.session.Turn++
			a.session.Append(Msg{Role: "user", Content: line})
			a.session.Save()
			a.logger.Emit("user", map[string]any{"turn": a.session.Turn, "text": line})
		}

		if a.pruner != nil && a.pruner.ShouldFire(a.session, a.lastPruneTokens) {
			uiInfo("pruning context...")
			next, err := a.pruner.Prune(ctx, a.session, a.lastPruneTokens)
			if err != nil {
				uiError(fmt.Errorf("prune skipped: %w", err))
			} else {
				a.lastPruneTokens = next
			}
		}

		turnCtx, turnCancel := context.WithCancel(ctx)
		cf := context.CancelFunc(turnCancel)
		a.turnCancel.Store(&cf)
		msg, err := a.chat(turnCtx, a.tools)
		if err != nil {
			a.turnCancel.Store(nil)
			turnCancel()
			uiError(err)
			readInput = true
			continue
		}
		a.session.Append(*msg)
		a.session.Save()
		if msg.Content != "" {
			a.logger.Emit("assistant_text", map[string]any{"turn": a.session.Turn, "text": msg.Content})
		}
		for _, tc := range msg.ToolCalls {
			a.logger.Emit("tool_call", map[string]any{
				"turn": a.session.Turn,
				"id":   tc.ID,
				"name": tc.Function.Name,
				"args": tc.Function.Arguments,
			})
		}

		if len(msg.ToolCalls) == 0 {
			a.turnCancel.Store(nil)
			turnCancel()
			a.logger.Emit("turn_end", map[string]any{"turn": a.session.Turn})
			readInput = true
			continue
		}

		readInput = false

		// Keep turnCtx live across tool execution so Ctrl-C cancels the
		// running tool (foreground exec, search) instead of just the chat
		// stream that already returned.
		for _, tc := range msg.ToolCalls {
			result := a.runTool(turnCtx, tc)
			a.session.Append(result)
			a.logger.Emit("tool_result", map[string]any{
				"turn":    a.session.Turn,
				"id":      tc.ID,
				"name":    tc.Function.Name,
				"content": result.Content,
			})
			// Stamp the assigned block ID into the content so the LLM can see
			// its own handle (#mN) and act on it immediately with park/forget.
			if id := a.session.Messages[len(a.session.Messages)-1].ID; id != "" {
				a.session.Messages[len(a.session.Messages)-1].Content = "[#" + id + "]\n" + result.Content
			}
			a.session.Save()
		}
		a.turnCancel.Store(nil)
		turnCancel()

		// Every tool batch keeps the loop running — its result needs reasoning.
		// The user only speaks again when the assistant emits content with no
		// tool calls (handled above).
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
		bgReg.killAll()
		a.session.Reset()
		initSessionMessages(a.session)
		a.tools = BuildTools(a.session)
		uiSessionNew()
		return true
	case s == "/undo":
		if e, ok := a.session.Undo(); ok {
			if err := writeBytesRaw(e.Path, []byte(e.Before)); err != nil {
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
		stopOnFirst := func() {
			if first {
				first = false
				stop()
			}
		}
		msg, err := a.client.ChatStream(ctx, a.session.ContextMessages(), tools,
			func(t string) {
				stopOnFirst()
				tokens <- t
			},
			func(t string) {
				stopOnFirst()
				uiReasoning(t)
			},
			func(name, delta string) {
				stopOnFirst()
				uiToolArgDelta(name, delta)
			},
			nil,
			func(phase string, duration time.Duration) {
				// Phase callback for tracking API phases
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

func (a *Agent) runTool(ctx context.Context, tc ToolCall) Msg {
	for _, t := range a.tools {
		if t.Name != tc.Function.Name {
			continue
		}
		input := json.RawMessage(tc.Function.Arguments)
		uiTool(tc.Function.Name, input)
		out, err := t.Fn(ctx, input)
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
