# Task: write a pytest suite

`calc.py` defines `divide(a,b)` and `is_prime(n)`. Write `test_calc.py` covering:

- divide: normal case, negative numerator, division-by-zero raises ValueError, truncation to 4 decimals.
- is_prime: 0/1/2/3/4 edge cases, a large prime (e.g. 7919), a large composite, non-int input returns False.

At least 8 separate test functions total. All must pass.

## Success
- `pytest -q test_calc.py` exits 0.
- `pytest -q test_calc.py` reports >= 8 passed.
- Do not modify `calc.py`.
