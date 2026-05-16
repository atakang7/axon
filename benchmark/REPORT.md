# Axon benchmark — first-pass report

Model: `openrouter/deepseek/deepseek-v3.2`, pruner disabled.
Each task ran non-interactively via `axon --prompt ... --log-json events.jsonl`
(new flag added in this session). 5 tasks per tier ran in parallel.

## Results at a glance

| Tier | Pass | Notes |
|------|------|-------|
| 1 (trivial) | 10/10 | Floor held. |
| 2 (basic) | 10/10 | Including SQLite, regex, JSON parsing, state machines. |
| 3 (multi-file / refactor) | 9/10 | One real failure (json→yaml); dead-code, rename, merge-resolve, http-server all pass. |
| 4 (hard / spec) | 7/10 | Three failures, each a distinct weakness — see below. |
| **Total** | **36/40** | |

## Failures and what they reveal

### Tier 3: `05_json_to_yaml` — overwrote the input file

Agent ran for **580s and 60+ tool calls** and still failed. The seed `data.json`
was replaced with what looks like a self-generated test fixture
(`{"key":"value:colon","ws":"  space",...}`). Net effect: the agent treated the
input file as scratch space, lost the original, and its own `j2y.py` then
produced YAML against the corrupted input.

Secondary bug in the produced YAML: it emits `null:\n  null` for a `null` key,
which is invalid (using `null` literal as a mapping key without quoting). The
agent never round-tripped its output through `yaml.safe_load` despite the task
explicitly stating that's the success check.

**Weakness: input/output discrimination.** With unstructured "do the task"
prompts, the agent will sometimes mutate seed files. There is no obvious
discipline that "files mentioned in the spec as input must not be modified."

### Tier 4: `04_job_queue_go` — counted submissions wrong

Output: `submitted=100 completed=100 failed=18 panicked=14`. The grader
expected `c+f+p == 100`, but `100+18+14 = 132`. The agent double-counted —
panicked and failed jobs were ALSO incremented in `completed`. Logical bug
in the stat accounting; the agent didn't write a test that would have caught
this.

**Weakness: composability of constraints.** Multi-property invariants
("totals add up to 100") aren't verified unless explicitly tested.

### Tier 4: `08_fix_broken_repo` — pytest collection error

`pytest -q` exits non-zero with: *"HINT: remove __pycache__ / .pyc files
and/or use a unique basename for your test file modules"*. The agent created
a duplicate `test_calc.py` at a different rootdir during exploration, so
pytest's import-mode finds two modules with the same basename.

The fix file (`calc.py`) itself looks correct (compares to expected behavior).
The agent broke the test runner by leaving stale artifacts somewhere in its
working tree. **Did not run `pytest -q` from the same cwd at the end** to
confirm the success criterion before declaring done.

**Weakness: stale artifact hygiene + final verification.** The task's success
criterion is a single command (`pytest -q exits 0`). Running that command
once at the end would have caught it.

### Tier 4: `10_minishell` — syntactically broken Python

The produced `minish.py` has a duplicate `def run(self, script_path):` line,
making the file un-importable. Yet the agent wrote ~10 helper test scripts
(`test_blank_lines.sh`, `test_cd.sh`, `test_comments.sh`, etc.) — clearly
spent its budget on test scaffolding while the core implementation was broken.
Either the agent never ran `python3 minish.py script.sh` end-to-end, or it
did, saw a syntax error, and didn't fix it before giving up.

**Weakness: long-horizon coherence.** When the spec demands many features
(builtins + externals + comments + args), the agent fragments work into
sub-tests and loses track of whether the main artifact even runs.

## Cost / token-bloat observations

Cost is supposed to be Axon's headline feature. The data:

- **Tier 1 trivial tasks averaged 12 tool calls each.** Median was 9.
  `02_word_count` used **32 tool calls** for a wc-clone; `09_dir_size` used
  **33** for a recursive size — both are ~5-line scripts. Most of the
  excess is `exec` calls re-verifying earlier steps.
- **Tier 3 `03_http_server` used 37 tool calls and 573 seconds.** Loops of
  curl / kill / restart while iterating on the server. Same "verify by
  running" thrash, just longer.
- **Tier 3 `05_json_to_yaml` used 60+ tool calls and still failed** — when
  the agent doesn't converge, it doesn't escalate (e.g. step back, replan)
  — it keeps issuing more execs.

Single-turn-only behavior was actually preserved: every task that succeeded
did so in **one user turn**, no clarifying-question regressions. That's a
non-trivial property (the prompt told it not to ask questions, and it
obeyed across 40 runs).

## Tooling weaknesses observed

- **Tool-call thrash on `exec`**: the agent re-runs verify commands even when
  prior runs already proved the artifact works. A "did I just verify this?"
  memo would cut Tier 1 cost in half.
- **`task` tool overuse on simple tasks**: tier3/02_write_test_suite issued
  6 `task` calls for an 8-test suite. The "register an objective" gate is
  intended to cap drift but is being used as scratch.
- **No "final smoke test" step**: tier4/08 and tier4/10 both failed because
  the agent never ran the literal success-criterion command at the end.
  Forcing that as the last action before turn-end would catch a class of
  silent failures.

## What's in this directory

- `tasks/tierN/<task>/TASK.md` — the spec.
- `tasks/tierN/<task>/_seed/` — frozen seed files (runner restores from
  here on every run).
- `tasks/tierN/<task>/events.jsonl` — JSONL event log (prompt, tool_call,
  tool_result, assistant_text, turn_end, done).
- `tasks/tierN/<task>/result.json` — per-task summary (turns, durations,
  tool sequence).
- `tasks/tierN/<task>/grade.json` — per-task verification (each success
  criterion checked individually).
- `tasks/tierN/grade_summary.json` and `tier_summary.json` — tier rollups.
- `run_tier.py` — parallel runner.
- `grade.py` — graders, one per task.

## How to re-run

```bash
# Per tier:
python3 benchmark/run_tier.py benchmark/tasks/tier4 5
python3 benchmark/grade.py    benchmark/tasks/tier4
```

Workspace is reset from `_seed/` on each run, so results are reproducible
modulo model nondeterminism.
