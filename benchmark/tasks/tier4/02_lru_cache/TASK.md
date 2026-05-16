# Task: LRU cache + tests

Implement `lru.py` exposing class `LRU(capacity: int)` with:
- `get(key)` → value or `None` if absent. Marks key as most-recently-used.
- `put(key, value)` — insert/update. If over capacity after insert, evict the least recently used. Updating an existing key counts as a use.
- `__len__`, `__contains__`.
- `keys_in_order()` → list of keys, most-recent first.

Constraints: stdlib only, no `functools.lru_cache`. O(1) `get`/`put` (use a doubly-linked list + dict, or `OrderedDict`).

Also write `test_lru.py` with at least 6 pytest tests covering: basic get/put, eviction order, update-counts-as-use, capacity 1, missing-key returns None, ordering after mixed ops.

## Success
- `pytest -q test_lru.py` passes ≥ 6 tests, exits 0.
- `python3 -c "from lru import LRU; c=LRU(2); c.put(1,'a'); c.put(2,'b'); c.get(1); c.put(3,'c'); print(c.keys_in_order())"` prints `[3, 1]`.
