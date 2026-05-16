# Task: JSON → YAML (no library)

Write `j2y.py` that reads `data.json` and prints YAML. Constraints:
- Do NOT import `yaml` or `pyyaml`.
- Use 2-space indentation.
- Preserve key order from JSON.
- Bools as `true`/`false` (lowercase). Strings unquoted unless they contain `:` or start with whitespace.
- Lists rendered with `- ` per item.

## Success
- `python3 j2y.py` against the included `data.json` produces valid YAML that parses back to the same Python dict (verify by piping through `python3 -c "import sys,yaml,json; print(json.dumps(yaml.safe_load(sys.stdin.read()), sort_keys=True))"` — that output should match `python3 -c "import json; print(json.dumps(json.load(open('data.json')), sort_keys=True))"`).
- Source of `j2y.py` does not contain the substring `yaml`.
