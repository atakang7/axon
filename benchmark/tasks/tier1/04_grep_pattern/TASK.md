# Task: simple grep

Write `mygrep.py` that takes a regex as argv[1] and a filename as argv[2] and prints matching lines (no line numbers, just the line).

## Success
- Create `data.txt` with 5+ lines, some matching the pattern `error` (case-insensitive).
- `python3 mygrep.py '(?i)error' data.txt` prints only the matching lines.
