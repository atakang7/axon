# Task: CSV stats

Given `data.csv` (columns: name, score), write `stats.py` that prints:
```
count: <n>
mean: <m rounded to 2 decimals>
median: <med rounded to 2 decimals>
stdev: <sd rounded to 4 decimals>
top3: <name1>,<name2>,<name3>
```
where `top3` are the 3 names with highest scores, in descending score order.

## Success
- `python3 stats.py` prints those 5 lines exactly in that format.
- Numeric values match what Python's `statistics` module computes.
