def divide(a, b):
    """Return a/b. Raises ValueError on b==0. Truncates to 4 decimals."""
    if b == 0:
        raise ValueError("division by zero")
    return round(a / b, 4)

def is_prime(n):
    """True if n is a prime integer >= 2."""
    if not isinstance(n, int) or n < 2:
        return False
    if n < 4:
        return True
    if n % 2 == 0:
        return False
    i = 3
    while i * i <= n:
        if n % i == 0:
            return False
        i += 2
    return True
