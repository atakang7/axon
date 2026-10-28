package axon

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Weaker models get the shape of tool calls wrong in two recurring ways, and
// each cost requests or ended the turn (GLM 5.3 Flash, eval 2026-10-28):
//
//   - An array or object argument sent as a JSON string: edits:"[{...}]".
//     The call failed to decode and the model retried, twice.
//   - A tool call written into the reply as a fenced JSON block instead of
//     called. The loop read it as the final answer and the turn ended on
//     its first request.

// coerceArgs decodes arguments that the tool's schema declares as arrays or
// objects but that arrived as strings holding JSON. Anything else is left as
// sent, so a genuine type error still reaches the tool and the model.
func coerceArgs(schema map[string]any, raw json.RawMessage) json.RawMessage {
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return raw
	}
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil {
		return raw
	}
	changed := false
	for name, v := range args {
		s, ok := v.(string)
		if !ok {
			continue
		}
		prop, _ := props[name].(map[string]any)
		typ, _ := prop["type"].(string)
		t := strings.TrimSpace(s)
		if (typ == "array" && strings.HasPrefix(t, "[")) || (typ == "object" && strings.HasPrefix(t, "{")) {
			var decoded any
			if json.Unmarshal([]byte(t), &decoded) == nil {
				args[name] = decoded
				changed = true
			}
		}
	}
	if !changed {
		return raw
	}
	out, err := json.Marshal(args)
	if err != nil {
		return raw
	}
	return out
}

var fencedBlock = regexp.MustCompile("(?s)```[a-zA-Z]*[ \\t]*\\n(.*?)```")

// toolArgKeys are argument names of the built-in tools; a JSON block in a
// reply that uses them is a call the model meant to make.
var toolArgKeys = []string{`"path"`, `"command"`, `"files"`, `"edits"`, `"old_str"`, `"query"`, `"pattern"`}

// looksLikeTextToolCall reports whether a reply without tool calls carries a
// tool call written as a fenced JSON block.
func looksLikeTextToolCall(content string) bool {
	for _, m := range fencedBlock.FindAllStringSubmatch(content, -1) {
		body := strings.TrimSpace(m[1])
		if !strings.HasPrefix(body, "{") && !strings.HasPrefix(body, "[") {
			continue
		}
		var v any
		if json.Unmarshal([]byte(body), &v) != nil {
			continue
		}
		for _, k := range toolArgKeys {
			if strings.Contains(body, k) {
				return true
			}
		}
	}
	return false
}

// textCallNote answers a reply that wrote a tool call instead of making it.
const textCallNote = "[runtime note: your last reply wrote a tool call as text in a code block. Text is not executed: nothing ran. Make the call through the tool-calling API now.]"
