def add(a, b):
    # fix: add instead of subtract
    return a + b

def mul(a, b):
    return a * b

def avg(xs):
    # fix: divide by correct length
    if len(xs) == 0:
        raise ValueError("Cannot compute average of empty list")
    return sum(xs) / len(xs)

def factorial(n):
    if n < 0:
        raise ValueError
    # fix: initialize to 1 for multiplication
    out = 1
    for i in range(1, n+1):
        out *= i
    return out