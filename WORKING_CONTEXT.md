# Working context

The model should see only what the work still needs, every request, without
ever stalling on its own housekeeping. Context is kept small continuously, by
two mechanisms running at two speeds, both anchored to one goal tree.

## Principles

1. **History is immutable; context is a view.** `Session.Messages` is never
   rewritten to save tokens. `ContextMessages` derives what the model sees.
   Every original stays reachable through `recall`.
2. **The disk is the source of truth.** A file's contents in context are a
   cache. A write's full `content` argument, or a read that a newer read of
   the same file replaced, is a stale cache entry.
3. **Relevance comes from the goal tree.** Every block belongs to a node of
   the tree below; whether a node is finished or active decides what happens
   to its blocks.
4. **Closed nodes are immutable.** A finished run or step never changes, so
   its card or note can be computed at any time and never goes stale.
5. **The critical path is deterministic and bounded.** No request waits on an
   unbounded model call to make room. Models improve quality in the
   background; rules guarantee progress.
6. **Models select, they do not rewrite.** A trim keeps exact line ranges of
   the original; code is never paraphrased into context. User words are kept
   verbatim.
7. **Changes are monotonic and near the tail.** Folding never unfolds, and
   trims touch recent blocks, so the provider's cached prompt prefix stays
   stable.

## The goal tree

Derived from the log on every request, nothing new is stored to build it:

```
session
├─ run 1  user message → agent work           finished  → run card
├─ run 2                                       finished  → run card
└─ run 3  (open)
   ├─ step 1  closed by task advance          finished  → step card
   ├─ step 2  closed by task advance          finished, newest → kept, trimmed
   └─ step 3  active                           blocks shown; enricher trims them
```

- A **run** starts at a user message. Folding state lives in `Session.Runs`.
- A **step** is the stretch of a run between successful `task` (`advance`,
  `replan`) or `compact` results. The step's description comes from the
  `register`/`replan` arguments in the log. Folding state lives in
  `Session.Steps`, keyed by the block that closed the step.
- Runs without a task plan have a single step: the run itself.

## Two speeds

| | Enricher (fast) | Pruning (milestones) |
|---|---|---|
| When | every ~3K tokens of new, unreviewed results in the open run | a step or run closes; budget pressure |
| Scope | only the new blocks, outside the tail | whole finished nodes |
| Who | one free model, background | rules (cards) + the consensus reviewers |
| Can | narrow a block to line ranges, matching lines, head/tail, or a one-line note | fold a node to its card; park blocks by unanimous vote |
| Cannot | remove a block outright, touch the tail, user messages or unresolved failures | — |
| Validity | per block, so a new user message never invalidates it | node is closed, so never stale |

Enricher syntax, one decision per line, every candidate exactly once:

```
m12 keep
m12 lines 40-85 120-140
m31 match "FAIL|panic" context 2
m31 head 30
m31 tail 40
m20 note "build ok; apk at app/build/outputs/apk/debug/app-debug.apk, 14MB"
```

Each line is validated in code: unknown ids, out-of-range lines, patterns that
match nothing, notes over 500 characters, and trims that would not shrink the
block are ignored (the block stays whole). A trimmed block renders as its
selected lines under a header naming what was kept and `recall <id>`.

## Layers in order (cheapest first)

1. Stale stubs, every projection, no cost.
2. Trims recorded by the enricher (`Session.Trims`).
3. Step folding: in the open run, finished steps older than the newest
   finished one fold to step cards; under pressure, all finished steps.
4. Run folding: finished runs beyond `keep_runs` (low 6, moderate 3,
   extreme 1) fold to run cards; under pressure, oldest first.
5. Run notes: one background note per finished run, grounded in its text.
6. Consensus reviewers over what is still unfolded.
7. Governor before every request: target 56K, limit 70K; folds runs then
   steps, waits ≤20s for a running review, one bounded review, then parks the
   oldest large results of the active step.

After any reduction the reviewers' growth baseline is reset to the new size;
otherwise a fold from 230K to 14K left the baseline at 230K and reviewers
never fired again in that session (seen 2026-10-24).

## Thought experiments

Checked against two real sessions (1586 and 619 messages, 2026-10-24).

1. **Long run without a task plan.** Runs 1–14 of the larger session never
   called `task`; one run was 185 KB over 57 results. Milestone pruning alone
   would do nothing there. The enricher reviews every ~3K tokens, so the
   active step stays trimmed; the governor remains the backstop.
   *Holds.*
2. **One step that never ends.** A single step reached 495 KB over 92
   results. Same answer: enricher continuously, governor parks oldest large
   results only under pressure. Step boundaries are a bonus, not the bound.
   *Holds.*
3. **Step 2 needs what step 1 read.** Folding step 1 at its milestone would
   force re-reads, the costliest measured behaviour (3–4 reads per file). So
   the newest finished step stays unfolded (only trimmed), and older ones
   fold. A file needed later is re-read once; the stale rule then hides the
   older copy. *Holds, with keep-one-step.*
4. **The enricher trims what the agent is about to edit.** Trims only touch
   blocks outside the tail (the agent has already acted on them), the prompt
   tells it to keep what the active step will edit, and the edit tool rejects
   an `old_str` that does not match the file, so a wrong trim costs one
   re-read, never a bad edit. *Holds.*
5. **The user types fast.** A new user message closes the run. Trims are per
   block and stay valid; run and step cards are of closed nodes. Nothing is
   thrown away, unlike the reviewers' whole-snapshot validation. *Holds.*
6. **Prompt cache.** Trims land just outside the tail, folds are monotonic
   and happen at milestones, so the cached prefix changes rarely and only near
   its end. *Holds.*
7. **Enricher cost.** ~3K tokens of new content per call plus the goal header:
   a 495 KB step costs about 45 small free calls, against main-model requests
   that each shrink by far more. *Acceptable.*
8. **Enricher answers garbage.** Every line is validated; anything invalid
   leaves the block whole. The worst case is no reduction. *Holds.*

## Guarantees

- No request is sent above the limit, and the turn does not fail just because
  history is long: folding always makes progress.
- The tail, the latest user message, unresolved failures and the task block
  are never folded, trimmed or stubbed.
- Every stub, trim and card names block ids; `recall` returns the original.
