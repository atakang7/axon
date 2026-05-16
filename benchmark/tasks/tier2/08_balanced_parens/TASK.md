# Task: balanced brackets

Write `balanced.py` exposing `balanced(s)` that returns True iff the string `s` has correctly matched `()`, `[]`, `{}` brackets (other characters ignored).

## Success
- `python3 -c "from balanced import balanced; print(balanced('([]{})'))"` → `True`
- `python3 -c "from balanced import balanced; print(balanced('([)]'))"` → `False`
- `python3 -c "from balanced import balanced; print(balanced('hello (world)'))"` → `True`
- `python3 -c "from balanced import balanced; print(balanced('('))"` → `False`
- `python3 -c "from balanced import balanced; print(balanced(''))"` → `True`
