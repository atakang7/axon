# Working context

The model should see only what the work still needs, at a size where every
request is cheap: tens of thousands of tokens, not hundreds. Everything else
waits in the log, one `recall` away.

## The rule

**Cheap models read; the main model decides.**

A cheap model never chooses what the agent does or what it forgets. It may
search and read, and it reports what it found as facts with exact quotes that
code checks against the files. Every judgement — what to change, what to keep
in context, when the work is done — belongs to the main model, or to a model
the user deliberately picked for that role.

Before v2.7 this rule was broken: free models voted on what to park, trimmed
blocks and wrote run notes. That took ~6,000 lines, needed consensus voting to
be safe, and still made the wrong call often enough to cost re-reads. It is
gone.

## Roles

| Role | Model | Can | Cannot |
|---|---|---|---|
| Main | `Config.Model` | read, write, run, decide | — |
| Checkpoint | `Config.Pruner`, default: the main model | choose what stays in context | act on the repository |
| Explorer | `Config.Explorer`, optional | read and search; report facts with quotes | write, run commands, recommend, decide |
| Code | — | cap tool output, take stale copies out, fold finished runs, verify quotes and ids | judge relevance |

## The log and the view

1. **History is immutable; context is a view.** `Session.Messages` is never
   rewritten to save tokens. `ContextMessages` derives what the model sees.
   Every original stays reachable through `recall`.
2. **The disk is the source of truth.** A file's contents in context are a
   cache. The same call repeated, or a read followed by a write in a closed
   run, makes the older copy stale; it leaves the context (code, no model).
3. **User messages are never dropped.** They are small, and they are the
   specification.
4. **Nothing repeats a shape.** Any structure that repeats is a template the
   model's attention is pulled toward. On 2026-10-26 a model that saw one
   card per finished run as its own turns started answering with cards
   instead of calling tools. So each thing is said once:
   - A folded request keeps the user's words and the final answer; what the
     work did is one line in **one history note** after the folded requests.
   - The folded steps of the open request share **one note**.
   - Results dropped by a checkpoint or the governor, and stale copies, leave
     the context **with their calls**; one **index** in the closing state
     block lists each by id. It sits at the end because it changes most, so
     the cached prefix before it survives.
   - Replies that only copied a runtime note are left out of the view.
   Runtime notes are user-role messages, never the assistant's.

## Checkpoints

A checkpoint is the deciding model choosing, in one tool call, what stays:

```
checkpoint {
  keep:  [{id: "m12"}, {id: "m31", lines: "40-85, 120-140"}],
  notes: [{id: "m14", note: "expiry compared in s vs ms at auth.go:132"}]
}
```

Every candidate it does not keep leaves the context with its call, and gets a
line in the index: its id, a gist code builds from the call (`read
auth/token.go`, `exec go test ./...`) and the note, if one was given.
Candidates are tool results outside the newest messages that are not
unresolved failures, not already folded or stubbed, and big enough to matter.

- **Forced.** Before a request, when the context estimate reaches the trigger
  (`pruner.mode`: low 48K, moderate 32K, extreme 20K; or
  `pruner.trigger_tokens`), the runtime sends the same context plus one note
  asking for the checkpoint call. With the main model as the checkpoint model
  the prefix is identical, so the request is served almost entirely from the
  provider's prompt cache: a checkpoint costs about one ordinary step.
- **Voluntary.** The tool is always available. The agent calls it when a step
  is done and its material is no longer needed.
- **Elastic.** If the work genuinely needs more than the trigger, the model
  keeps it, and the next forced checkpoint waits until the context has grown
  by `pruner.growth_tokens` (12K) beyond what was kept. A new user message
  resets this. The trigger cuts bloat, never the working set.
- **Validated by code.** Unknown ids, ranges outside a block and empty notes are
  ignored and reported back; nothing invalid is applied. If the checkpoint
  request fails twice, the deterministic floor below runs instead. A turn never
  stalls on its own housekeeping.

## The deterministic floor

Before every request, in order, without any model:

1. Stale copies leave the context; write bodies in closed runs are stubbed
   (always).
2. Finished runs beyond `keep_runs` (low 6, moderate 3, extreme 1) fold into
   the history note; finished task steps except the newest fold into the
   request's steps note.
3. Forced checkpoint, if over the trigger.
4. Only if the request is still above the hard limit (70K): fold the oldest
   runs and steps, then park the oldest large results of the current run.

## Exploration

`explore {question, paths}` hands a read-only question to the explorer model:
"where is token expiry set?", starting from `auth/`. It runs in its own
throw-away conversation with `read` and `search` only, within
`explorer.max_rounds` (16) and `explorer.timeout` (10 min), and ends by calling
`report`:

```
report {
  facts:     [{path, start_line, end_line, quote, claim}],
  not_found: [{what, searched}]
}
```

Code checks every quote against the file. A quote found elsewhere in the file
gets its line numbers corrected; a quote not in the file rejects the fact.
The main model receives only verified facts, the explorer's one-line reading
of each (labelled as its wording, not verified), and what was not found
(labelled as not proof of absence). None of the explorer's raw reading enters
the main context.

The main model can always read and search itself. Exploration is the cheap
path for broad questions, never a gate: it reads raw whatever it will edit.

## Guarantees

- No request is sent above the hard limit; housekeeping always makes progress.
- The newest messages, every user message, unresolved failures and the goal
  block are never dropped.
- Everything taken out is listed by its block id; `recall` returns the original.
- No cheap model's output reaches the main context unless code verified it
  against the repository.
