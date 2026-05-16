# Task: pretty-print JSON

Write `pretty.py` that reads JSON from stdin and writes it pretty-printed (2-space indent, sorted keys) to stdout.

## Success
- `echo '{"b":2,"a":1}' | python3 pretty.py` prints valid JSON with `"a"` before `"b"` and 2-space indents.
