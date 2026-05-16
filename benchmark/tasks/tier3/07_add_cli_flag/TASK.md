# Task: extend an existing CLI

Modify `script.py` to add a `--upper` flag that uppercases each line before printing. Keep the existing positional FILE argument and existing behavior unchanged when the flag is absent. Use `argparse` (replace the manual sys.argv parsing).

## Success
- `printf 'hello\nworld\n' > sample.txt && python3 script.py sample.txt` prints `hello\nworld\n` unchanged.
- `python3 script.py --upper sample.txt` prints `HELLO\nWORLD\n`.
- Running `python3 script.py` with no args exits non-zero (argparse default).
