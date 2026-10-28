# The harness: what each layer is for, and its target

Written 2026-10-28. The deciding model is cheap (GLM 5.x, MiMo-V2.6-Pro); the
goal is frontier-like results from it. A strong agent is a simple loop over
strong tools, with a context that only grows. Discipline that a frontier model
carries itself, a cheap model gets from the harness: in the tools' shape, in
what results say unasked, and in what the system already knows before the
first request. Notes that ask the model to behave are the last resort; every
note the model can miss is a rule the tools could have enforced.

Read with ROLES.md (who calls which model), WORKING_CONTEXT.md (checkpoints,
folding) and GOALS.md (the goal contract).

## Evidence that set the targets

The WSL android sessions of 2026-10-24 (pre-scout, pre-batching):

- 425 requests in 23 user messages; 85% of requests made exactly one tool call.
  At 10-30 s per request a turn was 30-50 minutes.
- One file read eight times in a turn, in 200-line pages (the read default).
- 70 of 228 commands began with the same `export ...`; every `cd` was lost.
- 13 directory listings, one level each, before the first useful read.
- `write` told the model a formatter runs after every write and to "emit
  content flat (no indentation)" for brace languages. No formatter existed.

## Layers

| Layer | Target | State after 2026-10-28 |
|---|---|---|
| Loop | Plain loop: request, run every call, append, repeat. Parallel read-only calls. | Unchanged; already this. |
| Knowing the project | Before the first request the model knows the layout, the build and test commands and the authors' instructions. | **New.** Workspace brief in the system message (`workspace_brief.go`): top level, commands detected from go.mod / package.json / gradle / Cargo / pyproject / Makefile, and AGENTS.md or CLAUDE.md. Computed once, so it is cached prefix. |
| Locating code | Deterministic and free: tree, declarations, search. Models only for broad questions. | **New.** `read` of a directory is a two-level tree; `read mode=outline` gives declarations with line numbers for a file or a whole directory (`outline.go`). Explorer and scout stay for broad questions. |
| Reading | Whole files once; never the same text twice. | **New.** Default read 500 lines (was 200). A read whose result is exactly a block still in full view returns a pointer to it (`read_dedup.go`). |
| Editing | One call per change, however many hunks and files; all or nothing. | **New.** `write edits:[{path, old_str, content}]` (`writeEdits`). |
| Verifying edits | Every write result says whether what it wrote still parses and builds. No decision, no request. | **New.** `afterWrite` (`write_check.go`): Go is gofmt'ed, syntax-checked in-process and its package built (vetted for test files); JSON parsed; Python compiled; JS `node --check`. The false formatter claim is gone. |
| Verifying the task | The model states the proof, code runs it, status is computed from output. | Unchanged: the goal contract (GOALS.md). |
| Shell | Like a terminal: cwd and exports persist; sensible defaults; output keeps what matters. | **New.** `shell_state.go`: cwd (inside the workspace) and exported variables carry between commands. `tail_lines` optional (default 80, was a hard error); default timeout 2 min (was 30 s: builds timed out and were retried); hidden output keeps its error lines. |
| Context | Append-only within a run, so every request hits the provider's prefix cache. Compaction is rare, explicit, and at a boundary. | **Changed.** The open run is never rewritten by policy: repeats are no longer stubbed mid-run, and finished steps fold only at the checkpoint trigger. Finished runs still fold between messages (batched, `minFoldBatch`). |
| Wire | Send the model only what it needs. | **Changed.** A reply's reasoning stays in the log and is never sent back (`wireMessages`); OpenRouter's `reasoning` delta is read as well as `reasoning_content`. |
| Measuring | Every change is run on fixed tasks: pass, wall time, requests, tokens, cache hit, cost. | **New.** `cortex/scripts/eval`: task fixtures with oracles and a runner. |

## First eval (2026-10-28, MiMo-V2.6-Pro, one run per cell)

| Task | Build | Pass | Seconds | Requests | Tool calls | Cost |
|---|---|---|---|---|---|---|
| go-pricing (two-bug fix) | before | yes | 158 | 16 | 14 | $0.0080 |
| go-pricing | after | yes | 122 | 11 | 10 | $0.0079 |
| py-report-format (feature + tests) | before | yes | 315 | 17 | 21 | $0.0175 |
| py-report-format | after | yes | 270 | 15 | 27 | $0.0202 |

One sample each, so read the direction, not the size. What the transcripts
showed: the Go fix was one `edits` call with `check: ok`; the largest single
cost in both builds is the goal contract itself (4 of 11 and 7 of 15
requests), mostly criteria recorded with a wrong expected string, then marked
unverifiable and recorded again. That is the next layer to simplify. The brief
also named `python` where only `python3` exists, so every Python oracle failed
to run; it now names the interpreter that runs.

## Behaviours a strong harness instills (observed from Claude Code, 2026-10-28)

Noted by the user while watching a frontier agent work: part of its quality
is the harness, not the model. What it asks for, and where we stand:

1. **A pinned plan, one step in progress.** Cortex has the task tool; the
   prompt asks for it only for multi-step work. Keep.
2. **Independent calls in one message.** Prompt and the gather nudge; the
   loop runs read-only calls in parallel. Keep.
3. **Edits are trusted.** "Do not re-read a file you just edited to verify:
   the edit would have errored." Now in the Cortex prompt, and backed by the
   check line on every write.
4. **Read before edit, from the file, not from memory.** `write` errors say
   this when old_str does not match.
5. **Verify by running, report faithfully.** The goal contract.
6. **Short progress updates to the user while working.** Not applicable to a
   one-shot loop; the VS Code activity list shows the work.

## Next, measured first

- A per-request list of which model ran (main, checkpoint, explorer) in the
  Context view (ROLES.md gap 3).
- LSP diagnostics instead of per-language commands, where a server is running.
- Project-configured checks (`checks: [{glob, command}]`) for languages the
  built-in checks do not cover (Kotlin, TypeScript).
- Grow the eval set from real sessions: every bad turn becomes a task.
