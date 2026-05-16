# Task: dedupe lines, preserve order

Write `dedupe.py` that reads stdin and writes each unique line to stdout in first-seen order.

## Success
- `printf 'a\nb\na\nc\nb\n' | python3 dedupe.py` prints `a\nb\nc\n`.
