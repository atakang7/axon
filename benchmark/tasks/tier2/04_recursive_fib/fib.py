def fib(n, memo=None):
    if memo is None:
        memo = {}
    if n in memo:
        return memo[n]
    if n == 0:
        return 0
    if n == 1:
        return 1
    result = fib(n - 1, memo) + fib(n - 2, memo)
    memo[n] = result
    return result
