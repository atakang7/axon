# Task: import dependency graph

Parse all `.py` files under `proj/`, find every `import X` line, and write `deps.txt` listing each edge as `from -> to`, sorted lexicographically.

## Success
- `deps.txt` exists with exactly these lines (sorted):
  ```
  a -> b
  a -> c
  b -> c
  b -> d
  c -> d
  ```
