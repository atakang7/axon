# Task: concurrent job queue in Go

Build a single-file `queue.go` (package `main`) implementing:
- A `Job` is `func() error`.
- A `Queue` with `Submit(j Job)` and `Wait()` that drains all submitted jobs.
- N worker goroutines (configurable; default 4). Workers pull from an internal channel.
- After all jobs complete OR a single job panics, `Wait()` returns. A job's panic is recovered and logged but does NOT crash the program.
- Failed jobs (returning error) and panicked jobs are counted; expose `Stats()` returning `{Submitted, Completed, Failed, Panicked int}`.

The `main()` of `queue.go` should:
1. Construct a queue with 4 workers.
2. Submit 100 jobs: every 7th panics, every 5th returns an error, rest succeed.
3. After Wait(), print stats as `submitted=100 completed=N failed=M panicked=K` to stdout.

## Success
- `go run queue.go` exits 0.
- Output line matches `^submitted=100 completed=\d+ failed=\d+ panicked=\d+$`, with submitted=100 and completed+failed+panicked == 100.
- Running it 3 times produces deterministic counts (failed and panicked are deterministic since job index decides them).
- No data race when run with `go run -race queue.go` (still exits 0).
