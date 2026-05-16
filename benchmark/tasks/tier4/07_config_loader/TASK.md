# Task: layered config loader

Write `config.py` exposing `load_config()` which builds the final config by merging, in order of increasing precedence:
1. `defaults.yaml` (shipped, in cwd)
2. `local.yaml` if it exists
3. Env vars: any var of the form `APP__<KEY>` or `APP__<SECTION>__<KEY>` overrides the matching dotted path. Values "true"/"false" → bool, all-digits → int, else string.

Nested dicts merge recursively (env override of `db.url` should not delete `db.pool`).

You may use `pyyaml` if installed, else fall back to a hand-rolled parser sufficient for the simple flat/nested YAML in `defaults.yaml`.

## Success
A test script:
```bash
set -e
echo 'port: 9090' > local.yaml
APP__DEBUG=true APP__DB__URL='postgres://x' python3 -c "import json,config; print(json.dumps(config.load_config(), sort_keys=True))"
```
must print exactly:
```json
{"db": {"pool": 5, "url": "postgres://x"}, "debug": true, "host": "localhost", "port": 9090}
```
