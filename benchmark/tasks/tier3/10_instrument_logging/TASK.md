# Task: instrument with logging

Modify `svc.py` so that each of `fetch`, `transform`, `save` logs (using the stdlib `logging` module at INFO level) entry like `enter <funcname> args=...` and exit like `exit <funcname> result=...`. Don't change return values or signatures.

Then add a `__main__` block that configures logging to print to stderr at INFO level and runs `pipeline(42)`.

## Success
- `python3 svc.py 2>&1 1>/dev/null | grep -c '^INFO'` returns `6` (3 enters + 3 exits, all on stderr).
- `python3 -c "from svc import pipeline; print(pipeline(7))"` prints `True` (no extra noise on stdout).
