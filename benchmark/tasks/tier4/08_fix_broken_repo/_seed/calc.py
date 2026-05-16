def add(a, b):
    # bug: subtracts instead of adds
    return a - b

def mul(a, b):
    return a * b

def avg(xs):
    # bug: off-by-one in division
    return sum(xs) / (len(xs) - 1)

def factorial(n):
    if n < 0:
        raise ValueError
    # bug: starts from 0 so result is always 0
    out = 0
    for i in range(1, n+1):
        out *= i
    return out
