# Task: implement a TODO CLI from spec

Build a multi-file Python CLI named `todo.py` that manages a JSON-backed todo list. Persistence file: `todos.json` in cwd.

## Commands
- `python3 todo.py add "<text>"` — append a todo, prints the new id (1-based, monotonic).
- `python3 todo.py list` — print one line per todo: `<id> [<x or space>] <text>` where `x` = done.
- `python3 todo.py done <id>` — mark as done. Unknown id → exit 1, stderr message.
- `python3 todo.py rm <id>` — remove. Ids of remaining items DO NOT renumber.
- `python3 todo.py clear` — remove all done items.

## Constraints
- Stdlib only.
- `todos.json` must be human-readable (indent=2).
- Empty/missing `todos.json` is treated as "no todos" — `list` prints nothing, exits 0.

## Success
Run this script and it must exit 0:
```bash
set -e
rm -f todos.json
python3 todo.py list
[ "$(python3 todo.py add 'buy milk')" = "1" ]
[ "$(python3 todo.py add 'walk dog')" = "2" ]
python3 todo.py done 1
out="$(python3 todo.py list)"
echo "$out" | grep -q '^1 \[x\] buy milk$'
echo "$out" | grep -q '^2 \[ \] walk dog$'
python3 todo.py rm 2
[ "$(python3 todo.py list | wc -l)" = "1" ]
python3 todo.py add 'task3'
python3 todo.py clear  # removes done id=1
out="$(python3 todo.py list)"
echo "$out" | grep -q '^3 \[ \] task3$'
[ "$(echo "$out" | wc -l)" = "1" ]
echo OK
```
