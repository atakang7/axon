# Roles: who does what in a conversation

The user's setup (2026-10-26): the deciding model is GLM 5.x; free models do
everything factual. This document is the target design, what exists, and the
gap, in the order to close it. Read it before changing who calls which model.

## The rule

**Free models find; GLM decides and writes; code checks and runs.**

Intelligence is needed at decision points, not in proportion to volume. Most
of a conversation's tokens are factual work: finding files, reading regions,
running chosen commands, pulling the failing lines out of a log. None of that
needs judgement, and none of it may set direction.

| Moment | Who | Why |
|---|---|---|
| Locate: where is X, who calls Y, how Z is configured | free explorer(s), parallel | factual; every quote checked against the file |
| Run what was chosen: a test, a build, an oracle | code | no model needed |
| Read a large output for what matters | code gate (errors, head/tail) | no model needed |
| Diagnose: from symptom to cause | GLM | judgement |
| Decide the change and its proof | GLM | judgement; the goal contract records it |
| Write the change | GLM | how the code is written is a decision |
| Checkpoint: what stays in context | GLM (the model doing the work) | needs the plan; same prefix is cached |
| Report | GLM | it knows what it did |

Two consequences:

1. **A free model never drives the loop.** Choosing what to read next is
   choosing direction. A free model answers a question GLM asked, within the
   scope GLM gave, and returns facts.
2. **GLM should rarely read to locate.** Every token GLM reads is paid at its
   price on every later request until a checkpoint. GLM reads raw only what it
   will edit or must compare against; everything else arrives as verified
   facts from explore.

## Caching, and why model switches are expensive

Providers cache a request's prefix and bill it at the cache-read price when
the same prefix comes again. The cache is per model. Switching the deciding
model mid-run makes the next request pay the whole context at full price, so
GLM drives the whole run; free models work in their own short conversations,
which never touch GLM's prefix.

## What exists (axon v2.7.1, cortex v2.21.1)

- `explore`: one explorer model, read and search only, facts with verified
  quotes, prose answers mined for verifiable quotes, parallel calls.
- Checkpoints by the deciding model; one history note, one steps note, one
  index of what is out; nothing repeats a shape.
- Gate: tool output caps; built-in search when rg is missing.
- Goal contract: criteria and oracles, status computed from the log.
- Prompt: explore for broad questions, read raw what you edit.

## The gap, in order

1. **Explorer pool with fallback.** Free models rate-limit and fail often. One
   explorer model makes `explore` fragile exactly when it is needed.
   `explorer.models: [a, b, c]` (cortex) / `Config.Explorers` (axon): try in
   order; a failed or empty exploration moves to the next; independent
   parallel questions spread over the pool. Spend stays free.
2. **Steer GLM to delegate locating.** When an explorer exists and GLM spends
   rounds searching and reading files it then does not edit, the next request
   carries a one-shot note: ask explore, read raw only what you will change.
   Deterministic, like the batching note in gather.go. The note fires at most
   once per run.
3. **Visibility.** The Context view and the bridge state show which model did
   each request (main, checkpoint, explorer) and the explorer's spend, so the
   split can be audited.
4. **Later, measured first:** delegate bulk mechanical edits (a rename across
   many files) to a free model from GLM's exact instructions, with the diff
   checked by the build and GLM's review. Only if the Context view shows edit
   output dominating GLM's spend.

Not in the plan: switching the deciding model by state (deciding / failing /
disputed). With GLM driving every request it is unnecessary, and switching
breaks the cache (see above). It becomes useful only for a GLM tier split
(e.g. a flash model for exploring rounds), and then only in whole-state
windows.
