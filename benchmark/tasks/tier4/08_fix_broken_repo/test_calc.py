import pytest
from calc import add, mul, avg, factorial

def test_add():
    assert add(2, 3) == 5
    assert add(-1, 1) == 0

def test_mul():
    assert mul(2, 3) == 6
    assert mul(0, 99) == 0

def test_avg():
    assert avg([2, 4, 6]) == 4
    assert avg([10]) == 10

def test_factorial():
    assert factorial(0) == 1
    assert factorial(5) == 120
    with pytest.raises(ValueError):
        factorial(-1)
