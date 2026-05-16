from collections import OrderedDict


class LRU:
    def __init__(self, capacity: int):
        if capacity <= 0:
            raise ValueError("capacity must be positive")
        self.capacity = capacity
        self.cache = OrderedDict()  # key -> value

    def get(self, key):
        """Return value for key, or None if not present. Mark as most recently used."""
        if key not in self.cache:
            return None
        # Move to end to mark as most recently used
        value = self.cache.pop(key)
        self.cache[key] = value
        return value

    def put(self, key, value):
        """Insert or update key with value. Update counts as use."""
        # If key exists, remove it first to update order
        if key in self.cache:
            self.cache.pop(key)
        # Insert at end (most recent)
        self.cache[key] = value
        # If over capacity, evict least recently used (first item)
        if len(self.cache) > self.capacity:
            self.cache.popitem(last=False)

    def __len__(self):
        return len(self.cache)

    def __contains__(self, key):
        return key in self.cache

    def keys_in_order(self):
        """Return list of keys, most-recent first."""
        # OrderedDict stores most recent at end, so we need reverse
        return list(reversed(list(self.cache.keys())))
