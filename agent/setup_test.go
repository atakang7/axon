package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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
