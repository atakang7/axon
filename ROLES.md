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

1. **Explorer pool with fallback.** *Done in axon v2.8.0.* Free models
   rate-limit and fail often. `explorer.models: [b, c]` (cortex, after
   `explorer.model`) / `Config.Explorers` (axon): each call starts at the next
   model, so parallel questions spread over the pool; a failed or unreported
   exploration moves to the next, and the result says which ones failed.
2. **Steer GLM to delegate locating.** *Done in axon v2.8.0.* After three
   replies in a row that only read and search, while an explorer exists, the
   next request carries a one-shot note: ask explore, read raw only what you
   will change. Once per run (gather.go, delegateTracker).
3. **Visibility.** *Partly done:* the Context view shows explorer spend, the
   explorer and its pool. Still to do: which model did each request (main,
   checkpoint, explorer), as a per-request list in the Context view.
4. **Later, measured first:** delegate bulk mechanical edits (a rename across
   many files) to a free model from GLM's exact instructions, with the diff
   checked by the build and GLM's review. Only if the Context view shows edit
   output dominating GLM's spend.

## The scout: every message starts free

*Done in axon v2.9.0.* The cheapest part of a turn is its beginning:
understanding the request and finding the code. So when an explorer pool
exists, each user message first goes to the **scout**: before the deciding
model's first request, the pool explores the request (with the last few
messages for context) in its own conversation and reports verified facts:
the files and functions involved, how the code behaves now, how the project
builds and tests that part. The brief enters the log as an ordinary explore
result, so the deciding model's first request already has it and can decide.

Why this and not a cheap model driving the turn and handing over:

- The scout's raw reading never reaches the deciding model, only verified
  facts. A cheap driver would put everything it read into the shared log,
  paid again by the deciding model on every request.
- The scout cannot change anything: explorers only read and search.
- No model switch, so no cache loss: the scout is a separate conversation.
- It reuses the pool, its fallback and quote checking.

Rules: skipped for messages under four words (acknowledgements, short
commands) and when `explorer.no_scout` is set; if every explorer fails the turn
starts without a brief. The deciding model can always explore or read more.

Not in the plan: switching the deciding model by state (deciding / failing /
disputed). The scout makes the cheap start structural; switching the driver
would break the cache and let a cheap model choose direction.
