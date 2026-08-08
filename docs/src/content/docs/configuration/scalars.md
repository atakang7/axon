---
title: Durations & byte sizes
description: Human-readable scalar formats accepted by axon.yaml.
---

YAML has no built-in duration or byte-size type, so Axon defines two scalar decoders for configuration readability.

## Durations

Duration fields accept Go-style duration strings:

```yaml
request_timeout: 30m
idle_timeout: 20s
timeout: 500ms
max_timeout: 1h30m
```

A bare number is interpreted as **seconds**:

```yaml
timeout: 45
# 45 seconds
```

Negative durations are rejected during YAML parsing.

When marshaled back through the type, Axon uses the standard human-readable duration string.

## Byte sizes

Byte-size fields accept decimal and binary suffixes.

### Decimal

```text
KB = 1000
MB = 1000²
GB = 1000³
```

### Binary

```text
KiB = 1024
MiB = 1024²
GiB = 1024³
```

Short `K`, `M`, and `G` forms use binary multipliers.

Examples:

```yaml
max_bytes: 12KB    # 12000
max_bytes: 2MB     # 2000000
max_bytes: 32KiB   # 32768
max_bytes: 2MiB    # 2097152
max_bytes: 4096    # 4096 bytes
```

Whitespace inside the scalar is removed and suffix matching is case-insensitive after normalization.

Negative sizes and unknown suffixes are rejected.

## Zero is usually treated as unset later

A scalar parser can represent zero, but `Settings.WithDefaults()` replaces non-positive configured duration/byte values for the fields it fills. In normal agent/client construction, zero therefore resolves to that field's default rather than “disabled” or “unlimited.”
