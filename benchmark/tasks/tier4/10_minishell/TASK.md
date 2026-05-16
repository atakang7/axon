# Task: tiny shell

Write `minish.py` — a non-interactive shell that reads a script from argv[1] and executes one command per line. Support:

- External commands: `cmd arg1 arg2 ...` runs via `subprocess.run`, inheriting stdout/stderr.
- Builtins:
  - `cd <dir>` — changes process cwd.
  - `set NAME=VALUE` — sets shell variable (NOT env). Multiple sets accumulate.
  - `echo $NAME` — prints the shell-variable value (or empty line if unset).
  - `exit [code]` — terminates with given exit code (default 0). Subsequent lines ignored.
- Lines starting with `#` are comments. Blank lines ignored.
- A failing external command (non-zero exit) does NOT abort the script unless followed by `set -e` semantics — which we DON'T implement; just continue.

## Success
A `script.sh`:
```
# demo
echo $NAME
set NAME=alice
echo $NAME
cd /tmp
pwd
exit 0
```
running `python3 minish.py script.sh` prints (in order):
```
<blank line>
alice
/tmp
```
and exits 0. Exit code 7 example: a script ending in `exit 7` causes `python3 minish.py` to exit 7.
