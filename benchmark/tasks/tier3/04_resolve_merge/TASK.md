# Task: resolve merge conflict

`conflicted.py` has a Git merge conflict. Resolve it by keeping the FEATURE branch's design (the `greetings` dict approach with French support) — but also preserve the `Hi` fallback punctuation difference: when language is unknown, use `f"Hi, {name}"` (NO trailing exclamation), while known languages use `"!"` at the end.

## Success
- No conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`) remain in `conflicted.py`.
- `python3 -c "from conflicted import greet; print(greet('alice','en'))"` → `Hello, alice!`
- `python3 -c "from conflicted import greet; print(greet('alice','fr'))"` → `Bonjour, alice!`
- `python3 -c "from conflicted import greet; print(greet('alice','jp'))"` → `Hi, alice` (no trailing `!`)
- `python3 -c "from conflicted import shout; print(shout('alice'))"` → `HELLO, ALICE!`
