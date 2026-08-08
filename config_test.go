package axon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A trailing newline from a shell export must not leak into a path or a key —
// String is the only thing standing between os.Getenv and every consumer.
func TestStringTrimsWhitespace(t *testing.T) {
	t.Setenv("AXON_TEST_STRING", "  value with spaces \n\t")
	if got := String("AXON_TEST_STRING"); got != "value with spaces" {
		t.Fatalf("String did not trim: %q", got)
	}
}

// Int must never let a bad override break startup: unset, unparseable, or
// below the floor all fall back to the caller's default rather than panicking
// or propagating garbage into a Limits field.
func TestIntFallbackBehaviour(t *testing.T) {
	const key = "AXON_TEST_INT"
	cases := []struct {
		name     string
		set      bool
		value    string
		fallback int
		min      int
		want     int
	}{
		{"unset returns fallback", false, "", 42, 1, 42},
		{"unparseable returns fallback", true, "not-a-number", 42, 1, 42},
		{"below min returns fallback", true, "0", 42, 1, 42},
		{"valid and >= min returns parsed value", true, "7", 42, 1, 7},
		{"equal to min is accepted", true, "1", 42, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(key, tc.value)
			} else {
				t.Setenv(key, "")
			}
			if got := Int(key, tc.fallback, tc.min); got != tc.want {
				t.Fatalf("Int(%q, %d, %d) = %d, want %d", tc.value, tc.fallback, tc.min, got, tc.want)
			}
		})
	}
}

// DataDir's precedence decides where every session and background-shell log
// ends up; getting the order wrong silently relocates a deployment's state.
func TestDataDirPrecedence(t *testing.T) {
	t.Run("AXON_DATA_DIR wins outright", func(t *testing.T) {
		t.Setenv("AXON_DATA_DIR", "/explicit/data")
		t.Setenv("XDG_DATA_HOME", "/xdg/data")
		if got := DataDir(); got != "/explicit/data" {
			t.Fatalf("DataDir = %q, want /explicit/data", got)
		}
	})
	t.Run("XDG_DATA_HOME used when AXON_DATA_DIR unset", func(t *testing.T) {
		t.Setenv("AXON_DATA_DIR", "")
		t.Setenv("XDG_DATA_HOME", "/xdg/data")
		want := filepath.Join("/xdg/data", "agent")
		if got := DataDir(); got != want {
			t.Fatalf("DataDir = %q, want %q", got, want)
		}
	})
	t.Run("falls back to ~/.local/share/agent", func(t *testing.T) {
		t.Setenv("AXON_DATA_DIR", "")
		t.Setenv("XDG_DATA_HOME", "")
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			t.Skip("no home dir available in this environment")
		}
		want := filepath.Join(home, ".local", "share", "agent")
		if got := DataDir(); got != want {
			t.Fatalf("DataDir = %q, want %q", got, want)
		}
	})
}

// AXON_SESSION_PATH is the one override an embedder relies on to pin session
// storage regardless of DataDir/cwd — it must be honoured byte for byte.
func TestSessionPathHonoursOverride(t *testing.T) {
	t.Setenv("AXON_SESSION_PATH", "/wherever/i/said.json")
	if got := SessionPath(); got != "/wherever/i/said.json" {
		t.Fatalf("SessionPath = %q, want the verbatim override", got)
	}
}

// Without an override, the session must land under DataDir()/sessions and
// keep the .json suffix a fresh load path.
func TestSessionPathDefaultLocation(t *testing.T) {
	t.Setenv("AXON_SESSION_PATH", "")
	t.Setenv("AXON_DATA_DIR", "/data")
	got := SessionPath()
	wantDir := filepath.Join("/data", "sessions")
	if filepath.Dir(got) != wantDir {
		t.Fatalf("SessionPath = %q, want a file under %q", got, wantDir)
	}
	if filepath.Ext(got) != ".json" {
		t.Fatalf("SessionPath = %q, want a .json file", got)
	}
}

// The per-cwd session key must be stable for repeated calls from the same
// directory (or the "session" would move every run) and must differ for two
// directories that share a basename (or two projects called "app" would
// collide onto the same session file).
func TestSessionKeyForCwdStableAndDistinct(t *testing.T) {
	t.Setenv("AXON_SESSION_PATH", "")
	t.Setenv("AXON_DATA_DIR", "/data")

	root := t.TempDir()
	dirA := filepath.Join(root, "one", "app")
	dirB := filepath.Join(root, "two", "app")
	if err := os.MkdirAll(dirA, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirB, 0755); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	if err := os.Chdir(dirA); err != nil {
		t.Fatal(err)
	}
	firstA := SessionPath()
	secondA := SessionPath()
	if firstA != secondA {
		t.Fatalf("SessionPath is unstable for the same cwd: %q vs %q", firstA, secondA)
	}

	if err := os.Chdir(dirB); err != nil {
		t.Fatal(err)
	}
	pathB := SessionPath()
	if pathB == firstA {
		t.Fatalf("two differently-pathed dirs with the same basename collided on %q", pathB)
	}
	if filepath.Base(firstA) == filepath.Base(pathB) {
		t.Fatalf("basenames should differ by hash suffix: %q vs %q", filepath.Base(firstA), filepath.Base(pathB))
	}
}

// BackgroundLogRoot is keyed by PID because shells never outlive the process
// that spawned them; two PIDs must never resolve to the same directory or a
// fresh run could adopt a previous run's stale shell logs.
func TestBackgroundLogRootIsPIDScoped(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", "/data")
	a := BackgroundLogRoot(111)
	b := BackgroundLogRoot(222)
	if a == b {
		t.Fatalf("BackgroundLogRoot did not vary with pid: %q == %q", a, b)
	}
	if filepath.Base(a) != "111" {
		t.Fatalf("BackgroundLogRoot(111) = %q, want a path ending in 111", a)
	}
}

// With nothing configured, LoadLimits must produce every documented default —
// this is the contract that lets the runtime work out of the box.
func TestLoadLimitsDefaults(t *testing.T) {
	for _, k := range limitsEnvKeys {
		t.Setenv(k, "")
	}
	got := LoadLimits()
	want := Limits{
		ReadLines:          200,
		ReadMaxBytes:       2 * 1024 * 1024,
		ExecTimeout:        30 * time.Second,
		ExecMaxTimeout:     600 * time.Second,
		ExecOutputBytes:    12000,
		ExecTailLines:      50,
		ExecMaxTailLines:   500,
		BashOutputMaxBytes: 32 * 1024,
		SearchTimeout:      30 * time.Second,
		SearchMaxMatches:   100,
		SearchOutputBytes:  12000,
	}
	if got != want {
		t.Fatalf("LoadLimits() = %+v, want %+v", got, want)
	}
}

// Every AXON_* override must actually reach its Limits field, and the second
// fields must convert seconds to a time.Duration correctly — a units bug here
// would silently make every exec either instant-timeout or unkillable.
func TestLoadLimitsOverrides(t *testing.T) {
	for _, k := range limitsEnvKeys {
		t.Setenv(k, "")
	}
	t.Setenv("AXON_READ_LIMIT", "50")
	t.Setenv("AXON_READ_MAX_BYTES", "1024")
	t.Setenv("AXON_EXEC_TIMEOUT_SECONDS", "5")
	t.Setenv("AXON_EXEC_MAX_TIMEOUT_SECONDS", "60")
	t.Setenv("AXON_EXEC_OUTPUT_LIMIT", "999")
	t.Setenv("AXON_EXEC_TAIL_LINES", "10")
	t.Setenv("AXON_EXEC_MAX_TAIL_LINES", "20")
	t.Setenv("AXON_BASH_OUTPUT_MAX_BYTES", "2048")
	t.Setenv("AXON_SEARCH_TIMEOUT_SECONDS", "3")
	t.Setenv("AXON_SEARCH_LIMIT", "5")
	t.Setenv("AXON_SEARCH_OUTPUT_LIMIT", "4096")

	got := LoadLimits()
	want := Limits{
		ReadLines:          50,
		ReadMaxBytes:       1024,
		ExecTimeout:        5 * time.Second,
		ExecMaxTimeout:     60 * time.Second,
		ExecOutputBytes:    999,
		ExecTailLines:      10,
		ExecMaxTailLines:   20,
		BashOutputMaxBytes: 2048,
		SearchTimeout:      3 * time.Second,
		SearchMaxMatches:   5,
		SearchOutputBytes:  4096,
	}
	if got != want {
		t.Fatalf("LoadLimits() = %+v, want %+v", got, want)
	}
}

// An override below the documented minimum must be ignored in favour of the
// default, exactly like Int/String elsewhere — a stray AXON_READ_LIMIT=0 in
// an env file must not zero out every read.
func TestLoadLimitsIgnoresBelowMinimum(t *testing.T) {
	for _, k := range limitsEnvKeys {
		t.Setenv(k, "")
	}
	t.Setenv("AXON_READ_LIMIT", "0")
	got := LoadLimits()
	if got.ReadLines != 200 {
		t.Fatalf("LoadLimits().ReadLines = %d with AXON_READ_LIMIT=0, want default 200", got.ReadLines)
	}
}

var limitsEnvKeys = []string{
	"AXON_READ_LIMIT", "AXON_READ_MAX_BYTES",
	"AXON_EXEC_TIMEOUT_SECONDS", "AXON_EXEC_MAX_TIMEOUT_SECONDS",
	"AXON_EXEC_OUTPUT_LIMIT", "AXON_EXEC_TAIL_LINES", "AXON_EXEC_MAX_TAIL_LINES",
	"AXON_BASH_OUTPUT_MAX_BYTES",
	"AXON_SEARCH_TIMEOUT_SECONDS", "AXON_SEARCH_LIMIT", "AXON_SEARCH_OUTPUT_LIMIT",
}
