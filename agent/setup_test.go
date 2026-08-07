package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// scriptedModel is the whole reason Model is an interface: the turn loop, and
// construction with it, run with no network and no API key.
type scriptedModel struct{}

func (scriptedModel) Complete(context.Context, Request) (*Msg, error) {
	return &Msg{Role: "assistant", Content: "ok"}, nil
}

// baseConfig is a minimally valid Config whose session lands in a temp file, so
// a test never reads or writes the developer's real session.
func baseConfig(t *testing.T) Config {
	t.Helper()
	t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "session.json"))
	return Config{Model: scriptedModel{}, SystemPrompt: "you are a test agent"}
}

func okFn(context.Context, json.RawMessage) (string, error) { return "", nil }

var okSchema = map[string]any{"type": "object"}

// A tool missing Name, Schema or Fn must be rejected at New. The failure this
// prevents is specific: a nil Fn panics mid-turn, inside the embedder, after
// the model has already committed to the call and the user has already paid
// for the tokens that produced it.
func TestNewRejectsIncompleteTool(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool Tool
	}{
		{"no name", Tool{Schema: okSchema, Fn: okFn}},
		{"blank name", Tool{Name: "   ", Schema: okSchema, Fn: okFn}},
		{"no schema", Tool{Name: "deploy", Fn: okFn}},
		{"no fn", Tool{Name: "deploy", Schema: okSchema}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig(t)
			cfg.Tools = []Tool{tc.tool}

			a, err := New(cfg)
			if err == nil {
				a.Close()
				t.Fatal("New accepted an incomplete tool")
			}
			if !errors.Is(err, ErrInvalidTool) {
				t.Fatalf("got %v, want ErrInvalidTool", err)
			}
		})
	}
}

func TestNewAcceptsCompleteTool(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Tools = []Tool{{Name: "deploy", Description: "Deploy a service.", Schema: okSchema, Fn: okFn}}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New rejected a complete tool: %v", err)
	}
	defer a.Close()

	for _, tool := range a.tools {
		if tool.Name == "deploy" {
			return
		}
	}
	t.Fatal("the custom tool is not in the agent's toolset")
}

// The name check must not fire before the completeness check, and a collision
// with a built-in the agent still has is its own error.
func TestNewRejectsDuplicateOfBuiltin(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Tools = []Tool{{Name: "read", Schema: okSchema, Fn: okFn}}

	a, err := New(cfg)
	if err == nil {
		a.Close()
		t.Fatal("New accepted a tool colliding with a built-in")
	}
	if !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("got %v, want ErrDuplicateTool", err)
	}
}

// Excluding a built-in frees its name — that is what makes ExcludeBuiltins a
// way to replace a built-in rather than only to remove one.
func TestExcludedBuiltinFreesItsName(t *testing.T) {
	cfg := baseConfig(t)
	cfg.ExcludeBuiltins = []string{"read"}
	cfg.Tools = []Tool{{Name: "read", Schema: okSchema, Fn: okFn}}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("excluding a built-in did not free its name: %v", err)
	}
	defer a.Close()

	reads := 0
	for _, tool := range a.tools {
		if tool.Name == "read" {
			reads++
		}
	}
	if reads != 1 {
		t.Fatalf("found %d tools named read, want exactly the custom one", reads)
	}
}

// findTool locates a tool by name, failing the test rather than the caller
// panicking on a nil Fn.
func findTool(t *testing.T, a *Agent, name string) Tool {
	t.Helper()
	for _, tl := range a.tools {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("tool %q not found on agent", name)
	return Tool{}
}

// A bad Cwd must fail construction rather than silently leaving the agent
// rooted somewhere the caller did not ask for.
func TestNewRejectsInvalidCwd(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Cwd = filepath.Join(t.TempDir(), "does-not-exist")

	_, err := New(cfg)
	if err == nil {
		t.Fatal("New accepted a nonexistent Cwd")
	}
	if !strings.Contains(err.Error(), "set cwd") {
		t.Fatalf("error does not name the failing step: %v", err)
	}
}

// A valid Cwd must land on the session, since that is the only place tools
// read the working directory from.
func TestNewAppliesValidCwd(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Cwd = t.TempDir()

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New rejected a valid Cwd: %v", err)
	}
	defer a.Close()

	if a.Session().Cwd != cfg.Cwd {
		t.Fatalf("session.Cwd = %q, want %q", a.Session().Cwd, cfg.Cwd)
	}
}

// A caller-supplied Session must be used as-is (same pointer), not copied —
// otherwise Config.Session round-tripping through an embedder's own storage
// silently stops working.
func TestConfigSessionReusedAsIs(t *testing.T) {
	t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "unused.json"))
	sess := LoadOrCreateSession()

	cfg := Config{Model: scriptedModel{}, SystemPrompt: "you are a test agent", Session: sess}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if a.Session() != sess {
		t.Fatal("New did not reuse the supplied *Session; embedder-held state and agent-held state have diverged")
	}
}

// With no Config.Session, the default per-cwd session is loaded — this is
// what makes AXON_SESSION_PATH the one knob a test needs to stay off the
// developer's real session file.
func TestConfigNilSessionLoadsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "default.json")
	t.Setenv("AXON_SESSION_PATH", path)

	cfg := Config{Model: scriptedModel{}, SystemPrompt: "you are a test agent"}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if a.SessionPath() != path {
		t.Fatalf("SessionPath() = %q, want %q", a.SessionPath(), path)
	}
}

// A fresh session gets exactly one system message: the embedder's role text
// plus the tool catalog. An existing, non-empty session must not be
// re-seeded — otherwise resuming a conversation would silently prepend a
// second system message every time the process restarts.
func TestNewSeedsSystemPromptOnlyOnce(t *testing.T) {
	t.Run("fresh session", func(t *testing.T) {
		cfg := baseConfig(t)
		a, err := New(cfg)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer a.Close()

		if len(a.Session().Messages) != 1 {
			t.Fatalf("fresh session has %d messages, want 1", len(a.Session().Messages))
		}
		want := buildSystemPrompt(cfg.SystemPrompt)
		if got := a.Session().Messages[0].Content; got != want {
			t.Fatalf("system message = %q, want %q", got, want)
		}
	})

	t.Run("existing session is not re-seeded", func(t *testing.T) {
		t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "session.json"))
		sess := LoadOrCreateSession()
		sess.Append(Msg{Role: "user", Content: "hi"})

		cfg := Config{Model: scriptedModel{}, SystemPrompt: "you are a test agent", Session: sess}
		a, err := New(cfg)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer a.Close()

		if len(a.Session().Messages) != 1 || a.Session().Messages[0].Content != "hi" {
			t.Fatalf("New re-seeded an existing session: %+v", a.Session().Messages)
		}
	})
}

// Two agents in one process must not share a background-shell registry.
// Before shells were made per-Agent, one agent's Close (or Reset) could
// terminate another agent's background servers.
func TestTwoAgentsShareNoShellRegistry(t *testing.T) {
	cfgA := baseConfig(t)
	a, err := New(cfgA)
	if err != nil {
		t.Fatalf("New a: %v", err)
	}
	defer a.Close()

	t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "b.json"))
	cfgB := Config{Model: scriptedModel{}, SystemPrompt: "you are a test agent"}
	b, err := New(cfgB)
	if err != nil {
		t.Fatalf("New b: %v", err)
	}
	defer b.Close()

	startB := findTool(t, b, "exec")
	out, err := startB.Fn(context.Background(), json.RawMessage(`{"command":"sleep 5","run_in_background":true}`))
	if err != nil {
		t.Fatalf("start background shell on b: %v", err)
	}
	shellID := parseShellID(t, out)

	// a has no such shell; a.Close() must not disturb b's.
	if err := a.Close(); err != nil {
		t.Fatalf("a.Close: %v", err)
	}

	bashOutputB := findTool(t, b, "bash_output")
	out, err = bashOutputB.Fn(context.Background(), json.RawMessage(`{"shell_id":"`+shellID+`"}`))
	if err != nil {
		t.Fatalf("b's shell died after a.Close(): %v", err)
	}
	if !strings.Contains(out, "status: running") {
		t.Fatalf("b's shell not running after a.Close(): %s", out)
	}

	killB := findTool(t, b, "kill_shell")
	if _, err := killB.Fn(context.Background(), json.RawMessage(`{"shell_id":"`+shellID+`"}`)); err != nil {
		t.Fatalf("cleanup kill: %v", err)
	}
}

// parseShellID pulls "bash_N" out of a formatted exec/bash_output result.
func parseShellID(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if id, ok := strings.CutPrefix(line, "shell_id: "); ok {
			return strings.TrimSpace(id)
		}
	}
	t.Fatalf("no shell_id in tool output: %s", out)
	return ""
}

// Reset must kill shells, wipe the log back to one system message, and
// rebind the same tool set the agent was constructed with — including
// custom tools and exclusions. A Reset that forgot any of these would leave
// a "fresh" agent that still leaks state from the conversation it just
// wiped.
func TestResetRebuildsSystemPromptToolsAndShells(t *testing.T) {
	cfg := baseConfig(t)
	cfg.ExcludeBuiltins = []string{"write"}
	cfg.Tools = []Tool{{Name: "deploy", Schema: okSchema, Fn: okFn}}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	execTool := findTool(t, a, "exec")
	out, err := execTool.Fn(context.Background(), json.RawMessage(`{"command":"sleep 5","run_in_background":true}`))
	if err != nil {
		t.Fatalf("start background shell: %v", err)
	}
	shellID := parseShellID(t, out)

	a.session.Turn = 7
	a.Reset()

	if len(a.Session().Messages) != 1 {
		t.Fatalf("Reset left %d messages, want 1 (system)", len(a.Session().Messages))
	}
	want := buildSystemPrompt(cfg.SystemPrompt)
	if got := a.Session().Messages[0].Content; got != want {
		t.Fatalf("system message after Reset = %q, want %q", got, want)
	}

	names := map[string]bool{}
	for _, tl := range a.tools {
		names[tl.Name] = true
	}
	if names["write"] {
		t.Fatal("Reset restored an excluded built-in")
	}
	if !names["deploy"] {
		t.Fatal("Reset dropped the custom tool")
	}

	// KillAll terminates the process but leaves the registry entry so its
	// final status can still be read — so "gone" means status != running,
	// not that the id becomes unknown.
	bashOutput := findTool(t, a, "bash_output")
	out, err = bashOutput.Fn(context.Background(), json.RawMessage(`{"shell_id":"`+shellID+`"}`))
	if err != nil {
		t.Fatalf("bash_output on the pre-Reset shell: %v", err)
	}
	if strings.Contains(out, "status: running") {
		t.Fatalf("Reset left the pre-Reset background shell running: %s", out)
	}
}

// Reset must keep the session's on-disk path — otherwise every reset would
// silently relocate storage to the default per-cwd path.
func TestResetKeepsSessionPath(t *testing.T) {
	cfg := baseConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	want := a.SessionPath()
	a.Reset()
	if got := a.SessionPath(); got != want {
		t.Fatalf("Reset relocated the session: got %q, want %q", got, want)
	}
}

// Undo must restore the exact pre-edit bytes, name the reverted path, and
// leave an append-only breadcrumb so the model does not keep reasoning
// about contents the file no longer has.
func TestUndoRestoresFileAndAppendsNote(t *testing.T) {
	cfg := baseConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	path := filepath.Join(t.TempDir(), "f.txt")
	before := "original contents\n"
	if err := os.WriteFile(path, []byte("edited contents\n"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	a.session.RecordEdit(path, before)

	msgCountBefore := len(a.session.Messages)
	got, ok := a.Undo()
	if !ok {
		t.Fatal("Undo reported nothing to undo")
	}
	if got != path {
		t.Fatalf("Undo returned path %q, want %q", got, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(data) != before {
		t.Fatalf("restored contents = %q, want %q", data, before)
	}

	if len(a.session.Messages) != msgCountBefore+1 {
		t.Fatalf("Undo appended %d messages, want 1", len(a.session.Messages)-msgCountBefore)
	}
	last := a.session.Messages[len(a.session.Messages)-1]
	if !strings.Contains(last.Content, "reverted") || !strings.Contains(last.Content, path) {
		t.Fatalf("revert note = %q, missing path or 'reverted'", last.Content)
	}
}

// An empty ledger must report failure rather than a false success.
func TestUndoOnEmptyLedger(t *testing.T) {
	cfg := baseConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	before := len(a.session.Messages)
	path, ok := a.Undo()
	if ok || path != "" {
		t.Fatalf("Undo on empty ledger returned (%q, %v), want (\"\", false)", path, ok)
	}
	if len(a.session.Messages) != before {
		t.Fatal("Undo on empty ledger appended a message")
	}
}

// If the restore write itself fails, Undo must not tell the model the edit
// was reverted — that would be a lie the model then acts on.
func TestUndoReportsFailureWhenRestoreWriteFails(t *testing.T) {
	cfg := baseConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	// A path whose parent directory does not exist: WriteFileAtomic cannot
	// create it, so the restore write fails.
	path := filepath.Join(t.TempDir(), "missing-dir", "f.txt")
	a.session.RecordEdit(path, "before")

	before := len(a.session.Messages)
	got, ok := a.Undo()
	if ok || got != "" {
		t.Fatalf("Undo returned (%q, %v) on a failing restore, want (\"\", false)", got, ok)
	}
	if len(a.session.Messages) != before {
		t.Fatal("Undo appended a 'reverted' note despite the restore write failing")
	}
}

// Cd must resolve relative paths against the session's cwd, persist the
// result, and reject anything that is not an existing directory.
func TestCd(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg := baseConfig(t)
	cfg.Cwd = root
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	got, err := a.Cd("sub")
	if err != nil {
		t.Fatalf("Cd(relative): %v", err)
	}
	if got != sub {
		t.Fatalf("Cd(relative) = %q, want %q", got, sub)
	}
	if a.Session().Cwd != sub {
		t.Fatalf("Cd did not persist onto the session: %q", a.Session().Cwd)
	}

	if _, err := a.Cd(file); err == nil {
		t.Fatal("Cd accepted a file as a directory")
	}
	if _, err := a.Cd(filepath.Join(root, "does-not-exist")); err == nil {
		t.Fatal("Cd accepted a nonexistent directory")
	}
}

// Close must be safe to call more than once, and must kill background
// shells both times without panicking on the second call's already-empty
// registry.
func TestCloseIsIdempotent(t *testing.T) {
	cfg := baseConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// SessionPath and Session must agree: SessionPath is exactly Session().Path(),
// and Session() hands back the live object the agent itself mutates rather
// than a copy.
func TestSessionPathAndSessionAgree(t *testing.T) {
	cfg := baseConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if a.SessionPath() != a.Session().Path() {
		t.Fatalf("SessionPath() = %q, Session().Path() = %q", a.SessionPath(), a.Session().Path())
	}
	if a.Session() != a.session {
		t.Fatal("Session() does not return the agent's live *Session")
	}
}

// toolSpecs is the one place the execution layer crosses into the model
// layer, and it must be an allowlist by construction: reflecting over
// ToolSpec's fields means a field added to Tool (like Fn) does not silently
// start crossing the boundary — this test breaks the moment ToolSpec grows a
// field nobody reviewed for that.
func TestToolSpecShapeIsAnAllowlist(t *testing.T) {
	typ := reflect.TypeOf(ToolSpec{})
	wantFields := []string{"Name", "Description", "Schema"}
	if typ.NumField() != len(wantFields) {
		t.Fatalf("ToolSpec has %d fields, want %d (%v) — a new field must be deliberately reviewed for what it exposes to the model",
			typ.NumField(), len(wantFields), wantFields)
	}
	for i, name := range wantFields {
		if typ.Field(i).Name != name {
			t.Fatalf("ToolSpec field %d = %q, want %q", i, typ.Field(i).Name, name)
		}
	}
}
