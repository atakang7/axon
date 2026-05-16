# Task: rename a symbol across files

The repo (in cwd) has multiple Python files using a function `compute_total`. Rename it to `compute_grand_total` everywhere — definitions and call sites — without breaking anything.

## Success
- `grep -r 'compute_total' .` returns nothing (excluding TASK.md / _seed/).
- `python3 main.py` runs successfully and prints the same output as before the rename.
- All files that previously imported or defined `compute_total` now use `compute_grand_total`.
