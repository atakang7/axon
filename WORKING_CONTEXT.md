# Working context

The model should see only what the work still needs, every request, without
ever stalling on its own housekeeping. This replaces "prune when the budget is
hit" with a context that is kept small continuously.

## Principles

1. **History is immutable; context is a view.** `Session.Messages` is never
   rewritten to save tokens. `ContextMessages` derives what the model sees.
   Every original stays reachable through `recall`.
2. **The disk is the source of truth.** A file's contents in context are a
   cache. A write's full `content` argument, or a read that a newer read of
   the same file replaced, is a stale cache entry.
3. **The unit of meaning is a run:** one user message plus all the agent work
   that answered it. Agent loops produce hundreds of messages per user message;
   they are one flow, not hundreds of decisions.
4. **Closed runs are immutable.** Once the next user message arrives, the
   previous run never changes, so its summary can be computed at any time, in
   the background, and never goes stale. A fast-typing user cannot invalidate it.
5. **The critical path is deterministic and bounded.** No request waits on an
   unbounded model call to make room. Models improve quality in the
   background; rules guarantee progress.
6. **User words are kept verbatim.** Folding never paraphrases a request.
7. **Folding is monotonic.** Once a run is folded it stays folded, so the
   provider's prompt-cache prefix is stable.

## Layers (cheapest first)

| Layer | When | Cost | What |
|---|---|---|---|
| Stale stubs | every projection | none | duplicate reads and re-run commands → one-line stub; in closed runs also reads of files edited later and write `content` arguments |
| Run folding | turn start + budget | none | closed runs beyond `keep_runs` → user message verbatim + a run card (files read/written, commands, failures, final answer, block range) |
| Run notes | after each run closes, background | one free-model call | enriches the card with a grounded summary; applied when ready, never awaited |
| Open-run review | background, during a run | free models, consensus | the existing category reviewers, now only over the unfolded part |
| Governor | before every request | none / ≤ bounded | target 56K, limit 70K: fold oldest runs → wait ≤20s for a running review → one bounded review → deterministic park of the oldest large open-run results |

`keep_runs` follows the prune mode: low 6, moderate 3, extreme 1.

## Guarantees

- No request is sent above the limit, and the turn does not fail just because
  history is long: folding always makes progress because every closed run can
  shrink to its card.
- The current run's tail, the latest user message, unresolved failures and the
  task block are never folded or stubbed.
- Every stub and card names block ids; `recall` returns the original text.
