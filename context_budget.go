package axon

import (
	"context"
	"fmt"
	"strings"
)

// HardContextLimit is the main agent's input budget in estimated tokens,
// including tool schemas. Estimates come from the calibrated meter in
// token_meter.go, not from a fixed bytes-per-token guess.
const HardContextLimit = 70000
const contextBudgetTarget = 56000

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

// ensureContextBudget runs before every model request. In order:
//
//  1. Rules, no model: finished runs beyond keep_runs fold to cards, and
//     finished task steps except the newest fold to step cards.
//  2. Over the checkpoint trigger: the deciding model chooses what stays.
//  3. Still over the hard limit: fold the oldest runs and steps, then park the
//     oldest large results of the current run.
//
// Step 2 can fail or keep a large working set; step 3 still guarantees that
// no request is sent above the limit. See WORKING_CONTEXT.md.
func (a *Agent) ensureContextBudget(ctx context.Context, tools []Tool) error {
	model := modelName(a.model)
	estimate := func() (int, error) {
		return requestInputTokens(Request{Messages: a.session.ContextMessages(a.settings.Pruner.ContextWindow()), Tools: toolSpecs(tools)}, model)
	}
	a.foldByPolicy(ctx, estimate)
	a.foldStepsByPolicy(ctx, estimate)
	n, err := estimate()
	if err != nil {
		return err
	}
	if n >= a.checkpointTrigger() && a.forceCheckpoint(ctx, tools, n) {
		if n, err = estimate(); err != nil {
			return err
		}
	}
	if n <= HardContextLimit {
		return nil
	}
	a.emit(ctx, Event{Kind: KindInfo, Text: fmt.Sprintf("input budget: ~%d of %d tokens; folding finished work", n, HardContextLimit)})

	before, folded := n, 0
	for n > contextBudgetTarget && (a.foldOldestRun() || a.foldOldestStep()) {
		folded++
		if n, err = estimate(); err != nil {
			return err
		}
	}
	if n > HardContextLimit && a.parkOpenRunOverflow(estimate) > 0 {
		folded++
		if n, err = estimate(); err != nil {
			return err
		}
	}
	if folded > 0 {
		a.persistContext(ctx, before, n)
	}
	if n <= HardContextLimit {
		return nil
	}
	return fmt.Errorf("the current request alone is ~%d tokens, above the %d input budget, with every finished run folded and the current run's older results parked", n, HardContextLimit)
}

// foldByPolicy keeps only the newest KeepRuns finished runs unfolded. Runs
// too small to gain anything stay as they are.
func (a *Agent) foldByPolicy(ctx context.Context, estimate func() (int, error)) {
	closed := closedRuns(a.session)
	keep := a.settings.Pruner.keptRuns()
	if len(closed) <= keep {
		return
	}
	var due []run
	for _, r := range closed[:len(closed)-keep] {
		if !a.session.runFolded(r) && runBytes(a.session, r) > 1500 {
			due = append(due, r)
		}
	}
	if len(due) < minFoldBatch {
		return
	}
	before, _ := estimate()
	for _, r := range due {
		a.session.foldRun(r)
	}
	a.emit(ctx, Event{Kind: KindInfo, Text: fmt.Sprintf("context folded: %d finished runs to run cards; originals via recall", len(due))})
	after, _ := estimate()
	a.persistContext(ctx, before, after)
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

// parkOpenRunOverflow is the last resort when the current run alone is over
// budget: park its oldest large tool results, outside the tail and never an
// unresolved failure, until the request fits the target. Originals stay
// reachable through recall.
func (a *Agent) parkOpenRunOverflow(estimate func() (int, error)) int {
	s := a.session
	start := 0
	if runs := sessionRuns(s); len(runs) > 0 {
		start = runs[len(runs)-1].start
	}
	failures := unresolvedToolFailures(s)
	parked := 0
	for i := start + 1; i < len(s.Messages)-tailKeep; i++ {
		m := s.Messages[i]
		if m.Role != "tool" || m.ID == "" || m.Parked || failures[m.ID] || len(m.Content) < 1024 {
			continue
		}
		if s.Park(m.ID, truncateRunes(firstLine(strings.TrimPrefix(m.Content, "[#"+m.ID+"]\n")), 160), "context governor: current run over the input budget; original via recall") != nil {
			continue
		}
		parked++
		if n, err := estimate(); err != nil || n <= contextBudgetTarget {
			break
		}
	}
	return parked
}

func (a *Agent) persistContext(ctx context.Context, before, after int) {
	if err := saveSessionAtomic(a.session); err != nil {
		a.emit(ctx, Event{Kind: KindError, Err: err, Text: "session not persisted"})
	}
	a.emit(ctx, Event{Kind: KindPruneEnd, Prune: &PruneInfo{Before: before, After: after}})
}

// foldStepsByPolicy folds the open run's finished steps except the newest
// one: the step just closed is what the next step most likely builds on. It
// waits until the context reaches the checkpoint trigger: folding rewrites the
// middle of the open run, which costs the prompt cache for the rest of it, so
// it is done once, when it is needed, not after every step.
func (a *Agent) foldStepsByPolicy(ctx context.Context, estimate func() (int, error)) {
	if n, err := estimate(); err == nil && n < a.checkpointTrigger() {
		return
	}
	uses := toolUses(a.session)
	var finished []step
	for _, st := range openRunSteps(a.session, uses) {
		if st.closer != "" {
			finished = append(finished, st)
		}
	}
	if len(finished) < 2 {
		return
	}
	var due []step
	for _, st := range finished[:len(finished)-1] {
		if stepBytes(a.session, st) > 1500 && !a.session.stepFolded(st) {
			due = append(due, st)
		}
	}
	if len(due) == 0 {
		return
	}
	before, _ := estimate()
	for _, st := range due {
		a.session.foldStep(st)
	}
	a.emit(ctx, Event{Kind: KindInfo, Text: fmt.Sprintf("context folded: %d finished steps to step cards; originals via recall", len(due))})
	after, _ := estimate()
	a.persistContext(ctx, before, after)
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
