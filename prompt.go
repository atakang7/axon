package axon

import (
	"strings"
)

// buildSystemPrompt composes the system message: the embedder's role text,
// then the one rule about tools that the tool definitions themselves cannot
// express.
//
// It deliberately does NOT restate the tool catalog. Every request already
// carries the same tools in its "tools" field — name, description and full
// JSON Schema — put there by toolSpecs at the call boundary. Writing them
// into the system message as well bought nothing and was charged for twice:
// on cortex's seven built-ins the duplicate catalog was ~8.4KB against a
// ~2.2KB role prompt, so roughly 2,100 tokens of every single request, on
// every call of every turn, restated what the API field beneath it already
// said. The model reads the native definitions structurally; it does not need
// them again as prose.
//
// The instruction below stays, because it is the one thing the tools field
// genuinely cannot say. Some providers hand back a model's native tool-call
// syntax as ordinary content instead of parsing it into tool_calls, and a
// turn that receives one reads as "the model is done" — see unusableReply,
// which exists to catch exactly that. Telling the model to use the real API
// makes the leak rarer; catching it makes the leak survivable.
func buildSystemPrompt(rolePromptText string, _ []Tool) string {
	var b strings.Builder

	b.WriteString(strings.TrimRight(rolePromptText, "\n"))
	b.WriteString("\n\n# TOOL CALLING\n")
	b.WriteString("Invoke tools through the JSON tool-calling API, using the tool definitions supplied with this request.\n")
	b.WriteString("Do not write tool calls into your reply as text — no <tool_call> elements, no XML, no Markdown code blocks pretending to be calls.\n")
	b.WriteString("A call written as text is not executed. It is read as your final answer, and the turn ends there.\n")

	return strings.TrimRight(b.String(), "\n")
}
