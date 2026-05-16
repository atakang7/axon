# Task: tiny HTTP server

Write `server.py` (Python stdlib only — `http.server` is fine) that serves on `127.0.0.1:8765` with two routes:

- `GET /ping` → 200, body `pong`, content-type `text/plain`.
- `GET /sum?a=N&b=M` → 200, body `<a+b>` as integer, content-type `text/plain`. Missing/invalid params → 400.

Then write `verify.sh` that:
- Starts the server in background.
- Hits both endpoints with curl, plus the 400 case.
- Kills the server.
- Prints `OK` if all three checks pass, else `FAIL` and exits non-zero.

## Success
- `bash verify.sh` exits 0 and prints `OK`.
