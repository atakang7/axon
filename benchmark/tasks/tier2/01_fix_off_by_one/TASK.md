# Task: fix off-by-one (and edge case)

`buggy.js` defines `lastN(arr, n)` which should return the last `n` elements of `arr`. It is buggy.

## Success
- Fix `buggy.js` so:
  - `lastN([1,2,3,4,5], 2)` → `[4,5]`
  - `lastN([1,2,3], 10)` → `[1,2,3]` (n > length returns full copy)
  - `lastN([1,2,3], 0)` → `[]`
  - `lastN([], 3)` → `[]`
- Running `node buggy.js` prints those four expected outputs in order.
- Do not delete the `module.exports` line.
