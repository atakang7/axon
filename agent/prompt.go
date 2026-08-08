package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// prompt.go — system prompt construction.
//
// The prompt is the embedder's text plus the built-in tool catalog, and
// nothing else. The runtime has no opinion about what an agent is.
//
// It used to add more: a startup probe pass that shelled out to pwd, whoami,
// uname and git, and a two-level listing of the working directory. Both were
// product decisions wearing a runtime's clothes — they taxed every single
// model call with tokens the embedder never asked for and could not remove.
// An embedder that wants either can put it in SystemPrompt, where it is
// visible and priced deliberately.

// buildSystemPrompt composes the system message: the embedder's role text,
// then the catalog of tools the runtime attaches to every agent.
func buildSystemPrompt(rolePromptText string, toolset []Tool) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(rolePromptText, "\n"))
	b.WriteString("\n\n# AVAILABLE TOOLS\n")
	b.WriteString("You MUST use the official JSON tool calling API to invoke tools.\n")
	b.WriteString("DO NOT output raw XML (e.g., <tool_call>) or Markdown tool calls in your response.\n")
	b.WriteString("Any tool call written directly in the text content will be ignored.\n\n")

	for _, t := range toolset {
		schemaBytes, _ := json.MarshalIndent(t.Schema, "", "  ")
		fmt.Fprintf(&b, "## %s\n%s\nSchema:\n```json\n%s\n```\n\n", t.Name, t.Description, string(schemaBytes))
	}

	return strings.TrimRight(b.String(), "\n")
}
