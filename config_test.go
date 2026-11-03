package axon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// config.go no longer resolves settings — axon.yaml does — so what is left to
// test here is where state lands and how the tool caps are projected. Both are
// the kind of thing that breaks silently: a wrong session path does not error,
// it just loses your conversation.

// ---------------------------------------------------------------------------
// Where state lands
// ---------------------------------------------------------------------------

// A pinned path wins outright. An operator who names a session file means it.
func TestSessionFileHonoursAPinnedPath(t *testing.T) {
	cfg := SessionConfig{DataDir: "/data", Path: "/tmp/pinned.json"}

	if got := cfg.SessionFile(); got != "/tmp/pinned.json" {
		t.Fatalf("SessionFile() = %q, want the pinned path", got)
	}
}

// Without a pin, each working directory gets its own file under the data
// directory. That is what lets one agent hold a separate conversation per
// project.
func TestSessionFileIsPerWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	cfg := SessionConfig{DataDir: root}

	dirA := filepath.Join(root, "project-a")
	dirB := filepath.Join(root, "project-b")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Chdir(dirA)
	a := cfg.SessionFile()

	t.Chdir(dirB)
	b := cfg.SessionFile()

	if a == b {
		t.Fatalf("two directories share a session file: %q", a)
	}

	for _, path := range []string{a, b} {
		if !strings.HasPrefix(path, filepath.Join(root, "sessions")) {
			t.Errorf("session %q is not under the configured data dir", path)
		}
		if filepath.Ext(path) != ".json" {
			t.Errorf("session %q does not end in .json", path)
		}
	}
}

// The same directory must resolve to the same file across runs, or a session
// is lost every time the agent restarts.
func TestSessionFileIsStableForOneDirectory(t *testing.T) {
	cfg := SessionConfig{DataDir: t.TempDir()}
	t.Chdir(t.TempDir())

	if first, second := cfg.SessionFile(), cfg.SessionFile(); first != second {
		t.Fatalf("session file is not stable: %q then %q", first, second)
	}
}

// Two same-named directories in different places must not collide — the hash
// suffix is the only thing keeping them apart.
func TestSessionFileDistinguishesSameNamedDirectories(t *testing.T) {
	cfg := SessionConfig{DataDir: t.TempDir()}

	var paths []string
	for _, parent := range []string{t.TempDir(), t.TempDir()} {
		dir := filepath.Join(parent, "api")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		t.Chdir(dir)
		paths = append(paths, cfg.SessionFile())
	}

	if paths[0] == paths[1] {
		t.Fatalf("two directories named \"api\" share a session file: %q", paths[0])
	}
}

// Background logs are keyed by PID because shells never outlive the process
// that spawned them; a fresh run must not adopt a previous run's log files.
func TestBackgroundLogDirIsPIDScoped(t *testing.T) {
	cfg := SessionConfig{DataDir: t.TempDir()}

	a := cfg.BackgroundLogDir(111)
	b := cfg.BackgroundLogDir(222)

	if a == b {
		t.Fatalf("log dir did not vary with pid: %q", a)
	}
	if !strings.HasSuffix(a, "111") {
		t.Fatalf("BackgroundLogDir(111) = %q, want a path ending in 111", a)
	}
}

// An unset data_dir must still produce a usable absolute location rather than
// writing into the working directory.
func TestUnsetDataDirFallsBackToAnAbsoluteLocation(t *testing.T) {
	t.Chdir(t.TempDir())

	path := SessionConfig{}.SessionFile()
	if !filepath.IsAbs(path) {
		t.Fatalf("SessionFile() = %q, want an absolute path", path)
	}
}

func TestDefaultDataDirFollowsXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg/data")

	if got := defaultDataDir(); got != filepath.Join("/xdg/data", "agent") {
		t.Fatalf("defaultDataDir() = %q, want it under XDG_DATA_HOME", got)
	}
}

// ---------------------------------------------------------------------------
// Tool caps
// ---------------------------------------------------------------------------

// The projection from the nested config into the flat caps is pure plumbing,
// which is exactly why it is worth a test: a field wired to the wrong source
// compiles, runs, and quietly applies the wrong limit.
func TestToolsConfigProjectsEveryCap(t *testing.T) {
	tools := ToolsConfig{
		Read: ReadConfig{Lines: 11, MaxBytes: 12},
		Exec: ExecConfig{
			Timeout:      Duration(13 * time.Second),
			MaxTimeout:   Duration(14 * time.Second),
			OutputBytes:  15,
			TailLines:    16,
			MaxTailLines: 17,
			KillGrace:    Duration(18 * time.Second),
		},
		BashOutput: BashOutputConfig{MaxBytes: 19},
		Search: SearchConfig{
			Timeout:     Duration(20 * time.Second),
			MaxMatches:  21,
			OutputBytes: 22,
		},
	}

	want := Limits{
		ReadLines:          11,
		ReadMaxBytes:       12,
		ExecTimeout:        13 * time.Second,
		ExecMaxTimeout:     14 * time.Second,
		ExecOutputBytes:    15,
		ExecTailLines:      16,
		ExecMaxTailLines:   17,
		ExecKillGrace:      18 * time.Second,
		BashOutputMaxBytes: 19,
		SearchTimeout:      20 * time.Second,
		SearchMaxMatches:   21,
		SearchOutputBytes:  22,
	}

	if got := tools.limits(); got != want {
		t.Fatalf("limits() = %+v, want %+v", got, want)
	}
}

// DefaultLimits is what every tool falls back to, so it must be fully
// populated. A zero cap here is not "unlimited" — it is a tool that returns
// nothing.
func TestDefaultLimitsAreAllSet(t *testing.T) {
	l := DefaultLimits()

	checks := map[string]int{
		"ReadLines":          l.ReadLines,
		"ReadMaxBytes":       l.ReadMaxBytes,
		"ExecOutputBytes":    l.ExecOutputBytes,
		"ExecTailLines":      l.ExecTailLines,
		"ExecMaxTailLines":   l.ExecMaxTailLines,
		"BashOutputMaxBytes": l.BashOutputMaxBytes,
		"SearchMaxMatches":   l.SearchMaxMatches,
		"SearchOutputBytes":  l.SearchOutputBytes,
	}
	for name, v := range checks {
		if v <= 0 {
			t.Errorf("DefaultLimits().%s = %d, want a positive cap", name, v)
		}
	}

	durations := map[string]time.Duration{
		"ExecTimeout":    l.ExecTimeout,
		"ExecMaxTimeout": l.ExecMaxTimeout,
		"ExecKillGrace":  l.ExecKillGrace,
		"SearchTimeout":  l.SearchTimeout,
	}
	for name, v := range durations {
		if v <= 0 {
			t.Errorf("DefaultLimits().%s = %s, want a positive duration", name, v)
		}
	}

	if l.ExecTimeout > l.ExecMaxTimeout {
		t.Errorf("default ExecTimeout (%s) exceeds ExecMaxTimeout (%s)", l.ExecTimeout, l.ExecMaxTimeout)
	}
	if l.ExecTailLines > l.ExecMaxTailLines {
		t.Errorf("default ExecTailLines (%d) exceeds ExecMaxTailLines (%d)", l.ExecTailLines, l.ExecMaxTailLines)
	}
}

func TestStringTrimsWhitespace(t *testing.T) {
	t.Setenv("AXON_TEST_STRING", "  spaced  ")

	if got := String("AXON_TEST_STRING"); got != "spaced" {
		t.Fatalf("String() = %q, want it trimmed", got)
	}
}
