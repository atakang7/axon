# Task: recursive directory diff

Write `diff_dirs.py` that recursively compares `dir_a/` and `dir_b/`. Print a JSON object to stdout (single line) with these keys, each a sorted list of relative paths:
- `only_in_a`
- `only_in_b`
- `changed` (path exists in both, file contents differ)
- `same` (path exists in both, contents identical)

Compare by file content (read fully). Skip directories themselves; only files appear in the lists.

## Success
- `python3 diff_dirs.py` produces JSON. Parsing and validating that:
  - `only_in_a == ["only_a.txt"]`
  - `only_in_b == ["only_b.txt"]`
  - `changed == ["changed.txt"]`
  - `same == ["keep.txt", "sub/nested.txt"]`
