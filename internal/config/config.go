// Package config resolves every value axon takes from its environment:
// on-disk locations and the numeric limits that bound tool output.
//
// BOUNDARY RULE: config is the bottom layer. It imports nothing from this
// module, and it must stay that way — no Provider, no Session, no Tool. If a
// value here ever needs a type from a higher layer, the value belongs in that
// layer instead. Everything above may import config; config imports nobody.
//
// It lives under internal/ so that consumers of the public agent API (bouton
// and any other embedder) cannot reach it. The stable surface for them is the
// re-exported helpers in package agent.
//
// Every value has an AXON_* override so a single environment variable can
// retune a deployment without a rebuild, and a default that makes the runtime
// work with no configuration at all.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Environment primitives
// ---------------------------------------------------------------------------

// String returns the trimmed value of an environment variable. Trimming
// matters: a trailing newline from a shell export would otherwise become part
// of a path or an API key.
func String(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// Int returns key parsed as an integer, or fallback when it is unset,
// unparseable, or below min. A bad override never breaks startup — it is
// simply ignored in favour of the default.
func Int(key string, fallback, min int) int {
	raw := String(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min {
		return fallback
	}
	return n
}

func homeDir() string {
	if home, _ := os.UserHomeDir(); home != "" {
		return home
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// ---------------------------------------------------------------------------
// Locations — AXON_* override, then XDG, then a conventional default
// ---------------------------------------------------------------------------

// Dir returns where the runtime looks for providers.json and context.json.
func Dir() string {
	if dir := String("AXON_CONFIG_DIR"); dir != "" {
		return dir
	}
	if dir := String("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "agent")
	}
	return filepath.Join(homeDir(), ".config", "agent")
}

// DataDir returns where the runtime writes session files and background-shell
// logs.
func DataDir() string {
	if dir := String("AXON_DATA_DIR"); dir != "" {
		return dir
	}
	if dir := String("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "agent")
	}
	return filepath.Join(homeDir(), ".local", "share", "agent")
}

// ProvidersPath returns the providers.json the runtime reads at startup.
func ProvidersPath() string {
	if path := String("AXON_PROVIDERS_PATH"); path != "" {
		return path
	}
	return filepath.Join(Dir(), "providers.json")
}

// ProbesPath returns the context.json holding the startup probe list.
func ProbesPath() string {
	if path := String("AXON_CONTEXT_PROBES_PATH"); path != "" {
		return path
	}
	return filepath.Join(Dir(), "context.json")
}

// SessionPath returns the session file for the current working directory.
func SessionPath() string {
	if path := String("AXON_SESSION_PATH"); path != "" {
		return path
	}
	return filepath.Join(DataDir(), "sessions", sessionKeyForCwd()+".json")
}

// BackgroundLogDir returns the per-process directory holding background-shell
// logs. Keyed by PID because shells never survive the process that spawned
// them — a fresh run must not adopt a previous run's log files.
func BackgroundLogDir(pid int) string {
	return filepath.Join(DataDir(), "bg", strconv.Itoa(pid))
}

// sessionKeyForCwd derives a stable per-directory key so each working
// directory keeps its own session. The directory basename makes the file
// recognisable by eye; the hash suffix keeps two same-named directories
// apart. Falls back to "default" when cwd is unavailable.
func sessionKeyForCwd() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return "default"
	}
	if abs, err := filepath.Abs(wd); err == nil {
		wd = abs
	}
	sum := sha256.Sum256([]byte(wd))
	return filepath.Base(wd) + "-" + hex.EncodeToString(sum[:6])
}

// ---------------------------------------------------------------------------
// Limits
//
// Every one of these bounds how many tokens a single tool call can push into
// the model's context, or how long it can hold a turn. They are the runtime's
// only defence against one bad tool call costing a fortune, so each has a
// deliberately conservative default.
// ---------------------------------------------------------------------------

// ReadLines is the default number of lines a slice read returns.
func ReadLines() int { return Int("AXON_READ_LIMIT", 200, 1) }

// ReadMaxBytes caps a full read. Without it a 50MB log would be loaded into
// memory and split line-by-line straight into context.
func ReadMaxBytes() int { return Int("AXON_READ_MAX_BYTES", 2*1024*1024, 1) }

// ExecTimeoutSeconds is the default per-call foreground exec timeout.
func ExecTimeoutSeconds() int { return Int("AXON_EXEC_TIMEOUT_SECONDS", 30, 1) }

// ExecMaxTimeoutSeconds caps any timeout the model asks for. Without a
// ceiling one tool call could hold the turn for hours.
func ExecMaxTimeoutSeconds() int { return Int("AXON_EXEC_MAX_TIMEOUT_SECONDS", 600, 1) }

// ExecOutputBytes caps the output of a single foreground exec.
func ExecOutputBytes() int { return Int("AXON_EXEC_OUTPUT_LIMIT", 12000, 1) }

// ExecTailLines is the default number of trailing lines kept from a command
// whose output exceeded the cap.
func ExecTailLines() int { return Int("AXON_EXEC_TAIL_LINES", 50, 1) }

// ExecMaxTailLines caps the tail size the model may request.
func ExecMaxTailLines() int { return Int("AXON_EXEC_MAX_TAIL_LINES", 500, 1) }

// BashOutputMaxBytes caps one background-shell poll, so a noisy server cannot
// dump megabytes into context per read.
func BashOutputMaxBytes() int { return Int("AXON_BASH_OUTPUT_MAX_BYTES", 32*1024, 1) }

// SearchTimeoutSeconds bounds a single ripgrep invocation.
func SearchTimeoutSeconds() int { return Int("AXON_SEARCH_TIMEOUT_SECONDS", 30, 1) }

// SearchMaxMatches caps how many matches one search returns.
func SearchMaxMatches() int { return Int("AXON_SEARCH_LIMIT", 100, 1) }

// SearchOutputBytes caps the total byte size of a search result.
func SearchOutputBytes() int { return Int("AXON_SEARCH_OUTPUT_LIMIT", 12000, 1) }
