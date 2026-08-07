package agent

import (
	"strings"

	"github.com/atakang7/axon/internal/tools"
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
func buildSystemPrompt(rolePromptText string) string {
	return strings.TrimRight(rolePromptText, "\n") + "\n\n" + tools.Catalog()
}
