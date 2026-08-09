package axon

import (
	"strings"
	"testing"
)

// The system prompt had no test at all, which is how it came to carry a full
// duplicate of the tool catalog — every tool's name, description and JSON
// Schema, restated as prose in front of the same definitions the request
// already carries in its "tools" field. On a seven-tool agent that was ~8.4KB
// of duplicate against a ~2.2KB role prompt, paid on every call of every
// turn.
//
// These tests pin the contract that mistake violated: the role text goes
// through, the tool-calling rule goes through, and nothing about any specific
// tool does.

func promptFixtureTools() []Tool {
	return []Tool{
		{
			Name:        "read",
			Description: "Read one file, or list a directory.",
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
			},
		},
		{
			Name:        "write",
			Description: "Write to a file.",
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"mode": map[string]any{"type": "string"}},
			},
		},
	}
}

func TestBuildSystemPromptKeepsRoleTextFirst(t *testing.T) {
	const role = "You are a coding agent."

	got := buildSystemPrompt(role, promptFixtureTools())

	if !strings.HasPrefix(got, role) {
		t.Fatalf("role text must lead the system prompt, got:\n%s", got)
	}
}

// The rule below is the one thing the tools field cannot express: some
// providers hand a model's native tool-call syntax back as ordinary content
// instead of parsing it, and a turn that receives one reads as "the model is
// done". unusableReply catches that; this instruction makes it rarer.
func TestBuildSystemPromptStatesTheToolCallingRule(t *testing.T) {
	got := buildSystemPrompt("role", promptFixtureTools())

	for _, want := range []string{"tool-calling API", "not executed"} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt no longer states the tool-calling rule: missing %q", want)
		}
	}
}

// The regression guard. A tool's name, description or schema appearing here
// means the catalog is being paid for twice.
func TestBuildSystemPromptDoesNotRestateTheToolCatalog(t *testing.T) {
	tools := promptFixtureTools()

	got := buildSystemPrompt("You are a coding agent.", tools)

	for _, tool := range tools {
		if strings.Contains(got, tool.Description) {
			t.Errorf("tool %q's description is duplicated into the system prompt; "+
				"the request's tools field already carries it", tool.Name)
		}
	}

	// Schema fragments are the expensive half of the duplication and the
	// part most likely to creep back in via a well-meaning "just remind the
	// model of the shape" change.
	for _, fragment := range []string{`"type": "object"`, `"properties"`, "Schema:"} {
		if strings.Contains(got, fragment) {
			t.Errorf("tool schemas are duplicated into the system prompt (found %q); "+
				"toolSpecs already sends them on every request", fragment)
		}
	}
}

// The prompt must not grow with the toolset. This is the property that
// actually bounds the cost: a prompt that is the same size for two tools and
// for twenty cannot be restating them.
func TestBuildSystemPromptSizeIsIndependentOfToolCount(t *testing.T) {
	const role = "You are a coding agent."

	few := buildSystemPrompt(role, promptFixtureTools())

	many := promptFixtureTools()
	for i := range 20 {
		many = append(many, Tool{
			Name:        strings.Repeat("x", 40),
			Description: strings.Repeat("filler description ", 20),
			Schema:      map[string]any{"type": "object", "index": i},
		})
	}

	if got := buildSystemPrompt(role, many); got != few {
		t.Errorf("system prompt changed when tools were added:\n%d chars with 2 tools\n%d chars with 22",
			len(few), len(got))
	}
}
