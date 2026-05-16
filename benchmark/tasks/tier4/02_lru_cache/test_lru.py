import pytest
from lru import LRU


def test_basic_get_put():
    """Basic get/put operations."""
    c = LRU(3)
    c.put(1, "a")
    c.put(2, "b")
    c.put(3, "c")
    assert c.get(1) == "a"
    assert c.get(2) == "b"
    assert c.get(3) == "c"
    assert c.get(4) is None


def test_eviction_order():
    """Eviction removes least recently used when capacity exceeded."""
    c = LRU(2)
    c.put(1, "a")
    c.put(2, "b")
    # Access 1 to make it more recent
    c.get(1)
    # Add third, should evict 2 (least recent)
    c.put(3, "c")
    assert 2 not in c
    assert c.get(2) is None
    assert c.get(1) == "a"
    assert c.get(3) == "c"


def test_update_counts_as_use():
    """Updating an existing key counts as a use and moves it to most recent."""
    c = LRU(3)
    c.put(1, "a")
    c.put(2, "b")
    c.put(3, "c")
    # Update key 1, should become most recent
    c.put(1, "a2")
    # Add fourth, should evict 2 (least recent now)
    c.put(4, "d")
    assert 2 not in c
    assert c.get(1) == "a2"
    assert c.get(3) == "c"
    assert c.get(4) == "d"


def test_capacity_one():
    """LRU with capacity 1 works correctly."""
    c = LRU(1)
    c.put("x", 42)
    assert c.get("x") == 42
    c.put("y", 99)
    assert c.get("x") is None
    assert c.get("y") == 99


def test_missing_key_returns_none():
    """Missing key returns None, not exception."""
    c = LRU(5)
    assert c.get("nonexistent") is None
    assert "nonexistent" not in c
    # Multiple missing keys
    assert c.get(123) is None
    assert c.get(None) is None


def test_ordering_after_mixed_ops():
    """keys_in_order returns correct order after mixed operations."""
    c = LRU(3)
    c.put(1, "a")
    c.put(2, "b")
    c.put(3, "c")
    # Access 2 to make it most recent
    c.get(2)
    assert c.keys_in_order() == [2, 3, 1]
    # Update 1 to make it most recent
    c.put(1, "a2")
    assert c.keys_in_order() == [1, 2, 3]
    # Add new key, evict 3
    c.put(4, "d")
    assert c.keys_in_order() == [4, 1, 2]


def test_len_and_contains():
    """__len__ and __contains__ work as expected."""
    c = LRU(4)
    assert len(c) == 0
    c.put("a", 1)
    c.put("b", 2)
    assert len(c) == 2
    assert "a" in c
    assert "b" in c
    assert "c" not in c
    c.put("c", 3)
    c.put("d", 4)
    c.put("e", 5)  # evict 'a'
    assert len(c) == 4
    assert "a" not in c
    assert "e" in c


def test_negative_capacity_raises():
    """Capacity must be positive."""
    with pytest.raises(ValueError):
        LRU(0)
    with pytest.raises(ValueError):
        LRU(-1)
