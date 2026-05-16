# Task: flatten JSON

Write `flatten.py` that reads JSON from `input.json` and prints flattened keys with dot-paths, one per line, sorted, in `key=value` format. Lists become `key.0`, `key.1` etc.

## Success
- `python3 flatten.py` against the included `input.json` prints exactly:
  ```
  active=True
  tags.0=a
  tags.1=b
  user.addr.city=paris
  user.addr.zip=75001
  user.name=alice
  ```
