# Task: memoized fibonacci

Write `fib.py` exposing `fib(n)` that returns the n-th Fibonacci number (fib(0)=0, fib(1)=1). Implementation must be recursive AND memoized so that `fib(100)` returns instantly.

## Success
- `python3 -c "from fib import fib; print(fib(10))"` prints `55`.
- `python3 -c "from fib import fib; print(fib(100))"` prints `354224848179261915075` and returns within 1 second.
- Source contains the word `def fib` (i.e. it's actually a function, not a closed-form).
