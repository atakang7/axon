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

// PrunerInputLimit bounds a curator request. It is deliberately well above
// HardContextLimit: the curator must read the entire active context plus its
// own instructions in one request, and every configured curator model accepts
// 512K+ tokens. Paging is a last resort only above this ceiling.
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

// Normal overflow is handled by pruning, not a failed API call. This barrier
// takes ownership of a background pass and then runs bounded category passes.
func (a *Agent) ensureContextBudget(ctx context.Context, tools []Tool) error {
	model := modelName(a.model)
	estimate := func() (int, error) {
		return requestInputTokens(Request{Messages: a.session.ContextMessages(a.settings.Pruner.ContextWindow()), Tools: toolSpecs(tools)}, model)
	}
	n, err := estimate()
	if err != nil {
		return err
	}
	if n <= contextBudgetTarget {
		return nil
	}
	if n <= HardContextLimit && (a.pruner == nil || !a.pruner.ShouldFire(a.session)) {
		return nil
	}
	a.emit(ctx, Event{Kind: KindInfo, Text: fmt.Sprintf("input budget: ~%d of %d tokens; compacting context before continuing", n, HardContextLimit)})
	if a.pruner == nil {
		return fmt.Errorf("context budget reached but no pruner is configured")
	}
	if job := a.pruner.pending; job != nil {
		select {
		case result := <-job.done:
			job.done <- result
			a.categoryBoundary(ctx, false)
		case <-ctx.Done():
			a.pruner.stopCategoryJob()
			return ctx.Err()
		}
		n, err = estimate()
		if err != nil {
			return err
		}
		if n <= HardContextLimit {
			return nil
		}
	}
	pruneCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	previous := a.turnCancel.Load()
	cf := context.CancelFunc(cancel)
	a.turnCancel.Store(&cf)
	defer a.turnCancel.Store(previous)
	for pass := 0; pass < 3; pass++ {
		before := a.pruner.ContextTokens(a.session)
		after, pruneErr := a.pruner.Prune(pruneCtx, a.session)
		if pruneErr != nil {
			if n <= HardContextLimit {
				return nil
			}
			return fmt.Errorf("context compaction could not complete; history retained: %w", pruneErr)
		}
		a.emit(ctx, Event{Kind: KindPruneEnd, Prune: &PruneInfo{Before: before, After: after}})
		next, err := estimate()
		if err != nil {
			return err
		}
		if next <= HardContextLimit {
			return nil
		}
		if next >= n {
			break
		}
		n = next
	}
	return fmt.Errorf("context retained: reviewers could not safely reduce protected or necessary information below the %d input budget", HardContextLimit)
}
