---
title: Security Boundaries
description: Tool privileges and credential handling.
---

Built-in tools receive `Limits` rather than provider settings, but run with the host process's permissions.

```text
+----------+  limits  +---------------+  commands  +------+
| Settings | -------> | built-in Tool | ----------> | host |
+----------+          +---------------+             +------+
```

| Boundary | Behavior |
|----------|----------|
| `Config.Settings` | Stores provider credentials and runtime policy |
| `Limits` | Holds caps passed to built-in tools |
| `Tool.Fn` | Can access anything captured by a custom closure |
| `bash` | Runs host commands; not a sandbox |
| `MCPServer.Env` | Adds values to the subprocess environment |

## Limits

- Do not pass secrets to an agent whose shell tools can read the process environment.
- Run untrusted workloads inside your own sandbox with restricted file and network access.
