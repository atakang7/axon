# Goals: the conversation's contract

Every user message states or refines a goal. The goal tree is the source of
truth for why the agent is working, when it is finished, and how that is
proven. Planning (the `task` tool) is a detail below a goal, not the tree.

## Why

Measured in real sessions (2026-10-24): users repeated "apk yok" / "apk 0
byte" four times; asked "did you actually verify the microphone works?";
said "you changed nothing". In each case the agent declared done without a
check the user would accept. A check invented after the work, by exploring
again at the end, tends to confirm what the agent expects. The reference for
"done" has to be fixed before the change.

## Model

```
goal m210 "apk dostum"                     continues m180
├─ criteria
│   K1 phone apk has the local apk's size
│      oracle   : ssh phone stat -c %s ~/Download/app.apk
│      expect   : 14334212
│      baseline : #m182 "0"                 (observed before the change)
│      status   : open | met #m | failed #m | stale | unverifiable
│   K2 apk installs
│      oracle   : ssh phone pm install -r ~/Download/app.apk
│      expect   : Success
└─ constraints (user's words): "servis Windows'ta çalışacak" (m30)
```

- A goal is keyed by the user message that opened it. Each user message opens
  one; the agent may declare that a message continues an earlier goal instead.
- A criterion is a checkable statement with an **oracle** (a command whose
  output decides it, or an observation) and optionally an **expect** (text the
  oracle output must contain) and a **baseline** (block id plus a quote from it,
  observed before the change).
- Quotes are checked against the block they cite. A baseline or evidence quote
  that is not in its block is rejected.

## Status is computed, not claimed

Recomputed from the log on every request:

- **Command oracle.** The latest exec result whose command matches the oracle
  decides it (recorded before or after the criterion: freshness is what counts): `exit_code: 0` and the expect
  text present → met; otherwise failed. The agent does not have to report
  anything; running the oracle is the check.
- **Observation.** The agent cites an evidence block and a quote from it
  (`goal check`). The quote must be in the block.
- **Stale.** A met criterion turns stale when any file is written after its
  evidence. Changes invalidate proof.
- **Unverifiable.** The agent may say a criterion cannot be checked from its
  side, with a reason. It is reported, never silently passed.
- **Changed files need chosen criteria.** A goal whose runs wrote files but
  recorded no criteria is open, even if some command succeeded afterwards.
  Replaying two real sessions, 15 of 18 file-changing runs had a successful
  command after the last write, yet those are where users rejected the work:
  "a command passed" is not a check anyone chose. Two shapes of the project's
  own tests do prove a change without criteria (goal_proof.go, 2026-10-28):
  red to green (a test command failed in the goal before the last write and
  passes after it) and tests written (the goal changed a test file and a test
  command passes after the last write). The eval showed criteria ceremony
  costing up to half the requests of tasks whose proof was exactly that.
- **expect** is matched ignoring whitespace and case: agents write
  "ok example.com/x" for Go's "ok  	example.com/x".

## Where it acts

1. **Context.** A goal block is sent at the end of every request (where the
   task block was): the active goal's intent, constraints, criteria with status,
   evidence ids and baseline quotes, the plan, and other open goals. Baselines
   live in the criteria, so folding or trimming their blocks loses nothing.
2. **Turn end.** When the agent is about to end the turn while the active goal
   has open, failed or stale criteria, or wrote files with nothing verified, it
   gets one more request with a short note naming them: validate with the
   oracles, or say which are unverified and why. Once per turn, so it cannot
   loop.
3. **Interface.** Bridge state carries the goals; the editor shows criteria and
   their status, so the user sees what "done" means and can correct it early.

## Decisions

| Question | Decision | Why |
|---|---|---|
| New goal or refinement? | The agent decides (`goal continue`); default is a new goal per message | It reads the message; a wrong default is harmless because open goals stay visible |
| How hard to enforce? | One turn-end note per turn, plus the status visible to the user | Blocking can trap the agent on an uncheckable criterion; visibility makes skipping obvious |
| Who records criteria? | The agent, once it knows how the work will be checked; at the latest before finishing | It knows the domain; code validates every reference it cites. Requiring it before the first edit (a mid-run note) and a baseline before changing anything made agents explore without end: on 2026-10-25 a run spent ~80 replies gathering, ~37 of them hunting for a test runner to take a baseline. Both were removed. |
| Who decides met/failed? | Code, from oracle output | The point is to not trust the claim |

## Golden scenarios

Each is a test in `goals_test.go`.

1. **APK, the real failure.** Agent says "sent" with no oracle run → turn-end
   note. Oracle shows size 0 → K1 failed. After resending, oracle shows the
   right size → met; no further note.
2. **Bug fix with a red test.** Baseline quote "FAIL TestX" from the first run;
   oracle `go test ./...` expect "ok". Passes → met. Another write afterwards →
   stale → turn-end note.
3. **A question.** No writes, no criteria → no note, no ceremony.
4. **Cannot verify (microphone).** Agent marks K1 unverifiable with a reason →
   no note; the goal block and the editor show it as unverified.
5. **Fake evidence.** `goal check` quoting text that is not in the cited block
   → rejected, criterion stays open.
6. **Refinement.** Second message continues the first goal; its criteria and
   status carry over and show under the new message.
7. **Edits with no criteria.** An unrelated `ls` passes after the write → still
   a note; the agent records "still builds" with oracle `go build ./...` and runs
   it → settled.

## Rollout

Status computation and the goal block are always on. The turn-end note can be
disabled with `goals.disable_nudges: true`. Limits: matching is by command
text, so an oracle must be run as recorded; a test edited to pass is not yet
detected beyond the stale rule.
