# Task: dir size

Write `dirsize.py` that takes a directory path and prints the total size in bytes of all regular files under it (recursive).

## Success
- Create `sample/` with at least 3 files of known sizes across 2 nested levels.
- `python3 dirsize.py sample` prints an integer matching `du -sb sample | cut -f1`.
