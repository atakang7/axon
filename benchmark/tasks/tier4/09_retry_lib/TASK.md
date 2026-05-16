# Task: retry decorator + tests

Write `retry.py` exposing `@retry(times=3, exceptions=(Exception,), backoff=0.0, on_giveup=None)` decorator:
- Re-runs the wrapped function up to `times` total attempts (so `times=3` means 1 try + 2 retries).
- Only catches and retries the listed `exceptions` types; others propagate immediately.
- Sleeps `backoff * attempt` seconds between attempts (use `time.sleep`; allow monkeypatching).
- After exhausting, raises the last caught exception, AND if `on_giveup` callable provided, calls `on_giveup(exc)` first.

Also write `test_retry.py` with ≥ 5 pytest tests covering: succeeds on first try, succeeds after 1 retry, gives up after `times`, doesn't catch unrelated exceptions, calls `on_giveup`. Use `monkeypatch.setattr(time, "sleep", lambda *_: None)` so tests are instant.

## Success
- `pytest -q test_retry.py` passes ≥ 5 tests, exits 0.
- `python3 -c "from retry import retry; n=[0]
@retry(times=3)
def f():
    n[0]+=1; raise ValueError(n[0])
try: f()
except ValueError as e: print(e)
print(n[0])"` prints `3` and `3`.
