package axon

import (
	"context"
	"fmt"
)

// HardContextLimit is the main agent's input budget in estimated tokens,
// including tool schemas. Estimates come from the calibrated meter in
// token_meter.go, not from a fixed bytes-per-token guess.
const HardContextLimit = 70000
const contextBudgetTarget = 56000

// contextLowWater is where clearing stops once the context passed
// HardContextLimit. Clearing far below the limit makes it rare: each pass
// rewrites the middle of the context and costs the prompt cache once.
const contextLowWater = 36000

// minClearBytes is the smallest result worth clearing: its one-line stub is
// about 150 bytes.
const minClearBytes = 400

// PrunerInputLimit bounds a checkpoint request. It is above HardContextLimit
// because a checkpoint is asked for precisely when the context is large: it
// must see all of it to choose.
const PrunerInputLimit = 200000

// RequestInputBudget estimates req's input tokens with the meter's
// cross-model ratio. This is a local admission estimate, not billable usage.
func RequestInputBudget(req Request) (int, error) {
	return requestInputTokens(req, "")
}

func requestInputTokens(req Request, model string) (int, error) {
	n, err := requestPayloadBytes(req)
	if err != nil {
		return 0, err
	}
	return meter.tokens(model, n), nil
}

func requestLimit(req Request) int {
	if req.InputLimit > 0 {
		return req.InputLimit
	}
	return HardContextLimit
}

func checkRequestBudget(req Request, model string) error {
	n, err := requestInputTokens(req, model)
	if err != nil {
		return err
	}
	if limit := requestLimit(req); n > limit {
		return fmt.Errorf("estimated input %d tokens exceeds %d; compact before sending", n, limit)
	}
	return nil
}

// minFoldBatch folds finished runs two at a time under the keep-runs policy,
// so the provider's cached prefix changes every other run, not every run.
const minFoldBatch = 2

// ensureContextBudget keeps the next request under HardContextLimit. Below
// it nothing changes, so the cached prefix grows untouched. Above it, old
// tool results are cleared in place, stale copies first and then the oldest,
// down to contextLowWater; the user's words, the model's replies and every
// call stay. Folding whole finished requests is the last resort. See
// WORKING_CONTEXT.md.
func (a *Agent) ensureContextBudget(ctx context.Context, tools []Tool) error {
	model := modelName(a.model)
	estimate := func() (int, error) {
		return requestInputTokens(Request{Messages: a.session.ContextMessages(a.settings.Pruner.ContextWindow()), Tools: toolSpecs(tools)}, model)
	}
	n, err := estimate()
	if err != nil {
		return err
	}
	if n <= HardContextLimit {
		return nil
	}
	a.emit(ctx, Event{Kind: KindInfo, Text: fmt.Sprintf("input budget: ~%d of %d tokens; clearing old tool results", n, HardContextLimit)})
	before := n
	if a.clearOldResults(n) > 0 {
		if n, err = estimate(); err != nil {
			return err
		}
	}
	for n > contextBudgetTarget && (a.foldOldestRun() || a.foldOldestStep()) {
		if n, err = estimate(); err != nil {
			return err
		}
	}
	a.persistContext(ctx, before, n)
	if n <= HardContextLimit {
		return nil
	}
	return fmt.Errorf("the current request alone is ~%d tokens, above the %d input budget, with old results cleared and every finished request folded", n, HardContextLimit)
}

// clearOldResults clears tool results until the context is about
// contextLowWater: copies made stale by a later read or edit of the same file
// first, then the oldest. A cleared write also loses its body in the call
// (the file on disk has it), which is most of what writing a new file costs.
// The newest tailKeep messages, small results and unresolved failures stay.
// It returns how many it cleared.
func (a *Agent) clearOldResults(tokens int) int {
	s := a.session
	uses := toolUses(s)
	callBytes := map[string]int{} // what clearing takes out of each call
	for _, m := range s.Messages {
		for _, tc := range m.ToolCalls {
			callBytes[tc.ID] = len(tc.Function.Arguments) - len(stubCallArgs(tc).Function.Arguments)
		}
	}
	stale := staleResults(s) // already one line in the view: clearing gains nothing
	weight := func(m Msg) int { return len(m.Content) + callBytes[m.ToolCallID] }
	failures := unresolvedToolFailures(s)
	end := len(s.Messages) - tailKeep
	superseded := map[int]bool{}
	last := map[string]int{} // path -> index of the newest read of it
	for i, m := range s.Messages {
		if m.Role != "tool" {
			continue
		}
		u := uses[m.ToolCallID]
		for _, p := range writePaths(u) {
			if j, ok := last[p]; ok {
				superseded[j] = true
			}
		}
		for _, p := range readPaths(u) {
			if j, ok := last[p]; ok {
				superseded[j] = true
			}
			last[p] = i
		}
	}
	var order []int
	for pass := 0; pass < 2; pass++ {
		for i := 0; i < end; i++ {
			m := s.Messages[i]
			if m.Role != "tool" || m.ID == "" || m.Parked || stale[i] != "" || failures[m.ID] || weight(m) < minClearBytes || superseded[i] != (pass == 0) {
				continue
			}
			order = append(order, i)
		}
	}
	cleared := 0
	for _, i := range order {
		if tokens <= contextLowWater {
			break
		}
		m := s.Messages[i]
		if s.Park(m.ID, "", "cleared: context over the input budget") == nil {
			cleared++
			tokens -= contextTokens(weight(m))
		}
	}
	return cleared
}

// foldOldestRun folds the oldest finished run that is still shown in full.
func (a *Agent) foldOldestRun() bool {
	for _, r := range closedRuns(a.session) {
		if a.session.foldRun(r) {
			return true
		}
	}
	return false
}

func runBytes(s *Session, r run) int {
	n := 0
	for _, m := range s.Messages[r.start:r.end] {
		n += len(m.Content)
		for _, tc := range m.ToolCalls {
			n += len(tc.Function.Arguments)
		}
	}
	return n
}

func (a *Agent) persistContext(ctx context.Context, before, after int) {
	if err := saveSessionAtomic(a.session); err != nil {
		a.emit(ctx, Event{Kind: KindError, Err: err, Text: "session not persisted"})
	}
	a.emit(ctx, Event{Kind: KindPruneEnd, Prune: &PruneInfo{Before: before, After: after}})
}

// foldOldestStep folds the oldest finished step of the open run, the newest
// finished one included; used only under budget pressure.
func (a *Agent) foldOldestStep() bool {
	for _, st := range openRunSteps(a.session, toolUses(a.session)) {
		if a.session.foldStep(st) {
			return true
		}
	}
	return false
}

func stepBytes(s *Session, st step) int {
	return runBytes(s, run{start: st.start, end: st.end})
}
