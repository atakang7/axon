# Task: find dead functions

Across `main.py`, `mod_a.py`, `mod_b.py`, identify functions that are defined but never called (transitively from `main.py`'s top-level `print(caller())`). Write `dead.txt` listing each dead function as `module:function`, one per line, sorted alphabetically.

## Success
- `dead.txt` exists.
- Its contents (sorted) are exactly:
  ```
  mod_a:dead_helper
  mod_b:orphan
  ```
- Do not delete the dead functions, just identify them.
