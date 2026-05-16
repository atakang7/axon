# Task: URL validator

Write `urlcheck.py` exposing `is_valid(url)` that returns True iff:
- starts with `http://` or `https://`
- has a non-empty host (no spaces, contains a `.`)
- optional path is allowed
- no embedded whitespace anywhere

Must NOT use `urllib`/`urlparse`. Pure regex or manual parse only.

## Success
- `python3 -c "from urlcheck import is_valid; print(is_valid('https://example.com/x'))"` → `True`
- `python3 -c "from urlcheck import is_valid; print(is_valid('ftp://example.com'))"` → `False`
- `python3 -c "from urlcheck import is_valid; print(is_valid('http://no dots'))"` → `False`
- `python3 -c "from urlcheck import is_valid; print(is_valid('http://x.y'))"` → `True`
- `python3 -c "from urlcheck import is_valid; print(is_valid(''))"` → `False`
- Source must not contain `urllib` or `urlparse`.
