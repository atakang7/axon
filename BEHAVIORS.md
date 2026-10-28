# Behaviours: what the harness makes every model do

Written 2026-10-28. The goal is not to make one model work well. Models have
their own quality and their own habits. The goal is a harness whose behaviour
is so clear and so dominant that any model run inside it works the way a strong
engineer does, and so thinks better and gets better results.

A behaviour is enforced, in this order of preference:

1. **By structure**: the tool interface, or the loop's state. The model cannot
   skip it.
2. **By one fixed rule**: a fact computed from the log, answered with one note.
3. **By the prompt**: one sentence, for what structure cannot carry.

A rule that the model can miss belongs in a higher layer if it can go there.

## The behaviours

| Behaviour | What it means | Enforced by |
|---|---|---|
| Know the project | Start from the layout, build and test commands and the authors' instructions | Structure: the workspace brief in the system message (`workspace_brief.go`) |
| Use what you know | Standard tools, the ones every model is trained on | Structure: `read_file`, `edit`, `write_file`, `bash`, `grep`, `glob`, `todo_write` (`std_tools.go`). No modes, no custom ceremonies |
| Look before you change | Edit only what was read, as it is now | Structure: `edit`/`write_file` refuse a file not read in the session, or changed since it was read (`fileLedger`) |
| Look efficiently | Lookups together; never the same text twice | Structure: read-only calls in one reply run in parallel; an identical read returns a pointer (`read_dedup.go`). Prompt: one sentence |
| Plan multi-step work | The plan is visible and current | Structure: `todo_write`; the plan is shown at the end of every request. Prompt: one sentence |
| Change precisely | Smallest correct edit, several in one reply | Structure: exact-match `edit` with uniqueness; every edit reports `check: ok/FAILED` (gofmt + build, JSON, Python, JS) |
| Prove the change | A check runs after the last change | Rule: a turn that changed files and ran no passing check since gets one note (`verify.go`); the state is shown at the end of every request |
| Work like a terminal | cd and exports persist; long output keeps its errors | Structure: `shell_state.go`, `errorLines` |
| Report honestly | Outcome, changes, proof, what was not verified | Prompt |
| Survive the provider | Retries, stream errors, thinking-only replies, one sticky fast caching provider | Structure, invisible to the model (`agent.go` retries, cortex route ranking) |

## What was removed, and why

- **Write and read modes** (`replace_string`, `replace_lines`, `save`, `slice`,
  `full`, `outline`, `files:[...]`): a custom interface is one more thing a
  small model gets wrong. GLM 5.3 Flash sent `edits` as a string.
- **The goal tool** (criteria, oracle, expect, baseline): ceremony cost up to
  half the requests of a task, mostly re-recording criteria with a wrong
  expected string. Verification is now a fact the loop computes.
- **The checkpoint tool**: the model deciding what stays in its context is a
  task no small model does well. The runtime keeps the open run append-only and
  folds finished work deterministically.
- **recall**: with an append-only run the disk and the shell are the truth;
  read again, run again.
- **explore and the scout**: the most important part of the work, finding
  things, handed to a different and less reliable model, with latency before
  every message. `grep`, `glob` and `read_file` do it deterministically.
- **The gather and delegate nudges**: notes the model can miss.

## Measuring it

`cortex/scripts/eval/behaviors.py` reads each run's log: replies, calls per
reply, tool errors, repeated reads, whether a plan was written, and whether
the last change was followed by a passing check. Together with the hidden
tests (`grade.sh`), a harness change is good when behaviour and results
improve across the cheap models, not just one.
