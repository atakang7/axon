# Semantic pruning with independent reviewers

`PrunerSettings.Semantic` is opt-in. The default legacy windowing behavior is unchanged.
In semantic mode, old content remains visible until explicitly parked; `window_blocks`
protects recent messages rather than silently masking every older block. Use
`PrunerSettings.ContextWindow()` when projecting messages outside Agent.

`NewConsensusPruner(a, b)` runs two independently configured models concurrently.
Only the intersection of their validated JSON park lists is accepted. Two or three
reviewers are supported; all configured reviewers must respond successfully.
A timeout, malformed reply, or provider error skips the pass without parking anything.
Failures trigger a one-minute pool cooldown; a pass is also retried only after context
grows by `growth_tokens`. There is no paid fallback in the Cortex free pool.

Semantic candidates exclude all user/system messages, the recent tail, the latest
assistant, task-tool results and assistant tool-call envelopes. Observed tool errors
and nonzero exec exits are protected until an identical tool invocation succeeds.
This guard cannot infer all semantic failures from arbitrary text; remaining decisions
are model judgments, not guarantees. A reviewer only sees bounded head/tail excerpts,
the task, user requirements and recent direction. It is told to retain uncertain blocks.
At most 24 large blocks are reviewed per pass, each with at least 256 bytes to reclaim.

Parking retains raw content, IDs and tool associations in the session. Compact
breadcrumbs remain in context. The semantic-mode `recall` tool reads original content
in 12,000-character pages without changing its archived state. Historical file text
may be stale; read the current file before editing. Session updates use a temporary
0600 file plus rename and roll back in-memory metadata if persistence fails.

Suggested aggressive starting configuration (policy values, not measured optima):

```yaml
pruner:
  semantic: true
  mode: extreme
  window_blocks: 6
  floor_tokens: 6000
  growth_tokens: 3000
  max_tokens: 8192
  timeout: 45s
```

Pruner failures retain context and continue the agent. If free endpoints remain
unavailable, context can continue growing; this implementation does not promise to
fit arbitrarily long sessions into a fixed provider window. Consensus reduces the
effect of disagreement but cannot eliminate correlated mistakes. No cost/quality
improvement percentage is claimed from connection checks.

## Full-context category mode

`PrunerSettings.Categories` together with `Semantic` replaces excerpt-based
review with full active-context classification. Cortex enables both for its
free reviewer pool. The 24-candidate limit does not apply in this mode. Previously
parked blocks appear as their retained summaries, not as raw archived text.

Reviewers independently group every eligible block, explain task contribution,
and supply 1-4 exact source quotes per category. The runtime validates complete,
non-overlapping candidate coverage and verifies every quote against its source.
Normal/low modes can remove only entire non-contributing eligible categories;
extreme mode can also remove individual blocks from contributing categories.
Protected user/system messages, recent blocks, failures, tool-call envelopes and
the latest working checkpoint remain outside the removal set. Category names
need not match: deletion requires agreement on each immutable block ID.

Category review runs on a deep snapshot concurrently with the main agent. Results
are applied between tool rounds, never inside an in-flight request. Changes to
user requirements, task state, the snapshot prefix or pruning mode invalidate
the result. The runtime rechecks current protection rules at application time.
Pending work is canceled on turn end, interrupt handling, reset, session/model
switch or close. Review calls are bounded individually. Oversized full-context
reviews use the paging procedure below; no source text is silently truncated.
A rate limit or malformed response retains the context.

One extractive summary is attached to each agreed removal group; other members
refer to its block ID. The original source is always retained for `recall`.
The commit must shrink estimated prompt tokens after counting summary and
breadcrumb overhead, otherwise it is rolled back. This is a byte/4 estimate,
not a guarantee of reduced dollar cost after provider cache invalidation.
`category_review` in the saved session records the most recent complete plans,
applied IDs and before/after estimates. This audit is not injected into prompts.

The semantic-mode `compact` tool lets the agent record a bounded working
checkpoint with evidence IDs after a closed, summarizable phase with progress
and no stuck investigation. These are model assessments, not proven facts.
It requests a review without waiting for another growth increment, subject to
the minimum context floor and eligible candidates. Token-growth scheduling
remains available when the agent never calls the tool. The latest checkpoint
is protected and persisted as a normal tool result. This is a custom hybrid
inspired by SelfCompact and observation masking, not a reproduction of their
training or published benchmark results.

## 70,000 input admission budget

Every shipped Client checks an immutable 70,000-unit input budget before HTTP,
including messages, tools and response schema. Routing checks it before catalog
lookup too. Because models have different tokenizers, units conservatively count
serialized UTF-8 bytes plus 4096 framing reserve and per-message/tool reserves.
This is deliberately stricter than 70,000 native tokens, not an exact tokenizer.
Output `max_tokens` is a separate setting; this limit governs sent input context.

Above the 56,000 soft target the agent can compact early; above the hard budget
it waits for an existing review or runs category pruning before its next main
request. Up to three shrinking passes are allowed. A successful compaction
continues automatically, rather than surfacing a context-overflow API error.
If reviewers are unavailable or protected/necessary context cannot safely fit,
the session is retained and execution pauses with an explanation: the runtime
never bypasses the budget or blindly deletes requirements to force progress.

Oversized review input is partitioned without dropping source bytes. Two free
workers extract source-verified page summaries. Every eligible source page is
then reviewed with the global page memory. Both reviewers must approve removal
on every fragment of a block; a keep on any page retains the whole original.
All summary and review calls pass the same budget check. If the global digest
itself cannot fit, compaction retains history instead of truncating it.
Paging loses the advantage of simultaneous full attention to all raw text; it
is the explicit tradeoff required to respect the per-request hard budget.
