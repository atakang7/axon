# Task: extract a shared helper

`dup.py` has three near-identical functions. Refactor: introduce a single private helper `_report(items)` that builds the report, and rewrite each `report_*` function as a one-line call to it. The public API (`report_users`, `report_orders`, `report_errors` taking a list) must stay unchanged in behavior.

## Success
- `dup.py` defines a function whose name starts with `_report` (or `_render`).
- Each of `report_users`, `report_orders`, `report_errors` is at most 2 lines (def + return).
- Behavior unchanged: `python3 -c "from dup import report_users; print(report_users(['a','b']))"` prints `REPORT:\n========\n- a\n- b\n========`.
