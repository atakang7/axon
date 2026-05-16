# Task: topological sort

`graph.json` describes a DAG as adjacency lists (key → list of successors). Write `topo.py` that prints one valid topological order, one node per line.

When multiple orderings are valid, break ties alphabetically (i.e. among nodes with no remaining incoming edges, pick the alphabetically smallest first — Kahn's algorithm with a sorted ready set).

## Success
- `python3 topo.py` for the included graph prints exactly:
  ```
  a
  b
  c
  d
  e
  ```
