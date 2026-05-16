# Task: log parser

Write `parse.py` that reads `access.log` and prints, for each user that has at least one ERROR line, a line of the form:
```
<user>: <error_count>
```
sorted by error_count descending, then user ascending. Users with zero errors are omitted.

## Success
- `python3 parse.py` produces exactly that output for the included `access.log`.
- For the provided file, expected:
  ```
  bob: 2
  dave: 1
  ```
