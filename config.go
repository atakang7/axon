// config.go holds what is left of axon's relationship with its environment:
// where state is written, and the flat caps the tools obey.
//
// It used to resolve thirteen AXON_* variables into those caps. It no longer
// does. Every one of them now lives in axon.yaml, where it can be seen, and
// this file only converts what was loaded into the shapes the runtime uses.
//
// Four environment variables survive, and they are all the same kind of thing:
// they say *where something is*, never *what it contains*.
//
//	AXON_CONFIG        path to axon.yaml          (load.go)
//	AXON_ENV           path to the .env           (load.go)
//	AXON_DATA_DIR      where state is written
//	AXON_SESSION_PATH  pins one session file
//
// The last two override the config rather than defaulting under it, because
// their whole purpose is to redirect state without editing a file — a
// container mounting a volume, or a test that must not touch the developer's
// real session. That last case is not hypothetical: removing this override
// once made the test suite read and rewrite a live 235-message session.
//
// BOUNDARY RULE: nothing here reads a setting. If a value needs deciding, it
// is decided in settings.go and passed in.
package axon

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// String returns the trimmed value of an environment variable. Trimming
// matters: a trailing newline from a shell export would otherwise become part
// of a path.
func String(key string) string {
	return strings.TrimSpace(os.Getenv(key))
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
// Locations
// ---------------------------------------------------------------------------

// defaultDataDir is where state goes when the config does not say. It follows
// the XDG convention so it lands somewhere a user's tooling already knows to
// back up or ignore.
func defaultDataDir() string {
	if dir := String("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "agent")
	}

	return filepath.Join(homeDir(), ".local", "share", "agent")
}

// SessionFile returns the session file for the current working directory
// under the configured data directory.
//
// A pinned session.path in the config wins outright; otherwise each working
// directory gets its own file, which is what lets one agent hold a separate
// conversation per project.
func (s SessionConfig) SessionFile() string {
	if path := String("AXON_SESSION_PATH"); path != "" {
		return path
	}

	if s.Path != "" {
		return s.Path
	}

	return filepath.Join(s.dataDir(), "sessions", sessionKeyForCwd()+".json")
}

// BackgroundLogDir returns the per-process directory under which each shell
// registry gets its own subdirectory.
//
// Keyed by PID because shells never survive the process that spawned them: a
// fresh run must not adopt a previous run's log files. A registry must not
// write directly here either — shell IDs restart from one in every registry,
// so two registries sharing this directory would overwrite each other's logs.
func (s SessionConfig) BackgroundLogDir(pid int) string {
	return filepath.Join(s.dataDir(), "bg", strconv.Itoa(pid))
}

func (s SessionConfig) dataDir() string {
	if dir := String("AXON_DATA_DIR"); dir != "" {
		return dir
	}

	if s.DataDir != "" {
		return s.DataDir
	}

	return defaultDataDir()
}

// sessionKeyForCwd derives a stable per-directory key. The basename makes the
// file recognisable by eye; the hash suffix keeps two same-named directories
// apart. Falls back to "default" when the working directory is unavailable.
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

// SessionPath returns the default session file for the current working
// directory, using the default data location.
//
// It exists for callers that have no Settings in hand — Session.Reset, and any
// embedder inspecting where state would land. Anything constructed through
// New uses the configured location instead.
func SessionPath() string {
	return SessionConfig{}.SessionFile()
}

// ---------------------------------------------------------------------------
// Limits
// ---------------------------------------------------------------------------

// Limits is the flat set of caps a tool obeys, projected from ToolsConfig by
// ToolsConfig.limits.
//
// It stays a separate, flatter type from the config for one reason: a tool
// must not be able to reach the provider credentials. Handing a tool the whole
// Settings would give every one of them a path to the API key, enforced by
// nothing but everyone's good intentions. Handing it Limits makes that
// impossible to write.
//
// It is resolved once, when an agent is constructed, and passed down. Reading
// settings at call depth would make every tool's behaviour depend on ambient
// state, so that two agents in one process could not be tuned differently —
// which is the exact problem this file used to have.
type Limits struct {
	// ReadLines is the default number of lines a slice read returns.
	ReadLines int

	// ReadMaxBytes caps a full read.
	ReadMaxBytes int

	// ExecTimeout is the default foreground exec timeout.
	ExecTimeout time.Duration

	// ExecMaxTimeout caps any timeout the model asks for.
	ExecMaxTimeout time.Duration

	// ExecOutputBytes caps the output of a single foreground exec.
	ExecOutputBytes int

	// ExecTailLines is the default number of trailing lines kept from a
	// command whose output exceeded the cap.
	ExecTailLines int

	// ExecMaxTailLines caps the tail size the model may request.
	ExecMaxTailLines int

	// ExecKillGrace bounds how long a killed command's output copier is
	// waited on before it is abandoned.
	ExecKillGrace time.Duration

	// BashOutputMaxBytes caps one background-shell poll.
	BashOutputMaxBytes int

	// SearchTimeout bounds a single ripgrep invocation.
	SearchTimeout time.Duration

	// SearchMaxMatches caps how many matches one search returns.
	SearchMaxMatches int

	// SearchOutputBytes caps the total byte size of a search result.
	SearchOutputBytes int
}

// DefaultLimits returns the caps an agent uses when no configuration is
// supplied. It is the tool section of DefaultSettings, projected.
func DefaultLimits() Limits {
	return DefaultSettings().Tools.limits()
}
