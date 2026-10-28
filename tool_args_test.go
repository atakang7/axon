package axon

import (
	"encoding/json"
	"testing"
)

func TestCoerceArgsDecodesArraysSentAsStrings(t *testing.T) {
	schema := obj("object", props{
		"path":  strSchema("p"),
		"edits": arr(obj("object", props{"old_str": strSchema("o")}, nil)),
	}, nil)
	got := coerceArgs(schema, json.RawMessage(`{"path":"a.go","edits":"[{\"old_str\":\"x\",\"content\":\"y\"}]"}`))
	var args struct {
		Path  string
		Edits []writeEdit
	}
	if err := json.Unmarshal(got, &args); err != nil || len(args.Edits) != 1 || args.Edits[0].OldStr != "x" || args.Path != "a.go" {
		t.Fatalf("coerced %s (%v)", got, err)
	}
	plain := json.RawMessage(`{"path":"[not json"}`)
	if string(coerceArgs(schema, plain)) != string(plain) {
		t.Fatal("a string field must stay as sent")
	}
}

func TestTextToolCallIsRecognized(t *testing.T) {
	if !looksLikeTextToolCall("First, I list the files.\n\n```json\n[\n  {\"path\": \"read\", \"mode\": \"outline\"}\n]\n```") {
		t.Fatal("a fenced JSON call was not recognized")
	}
	if looksLikeTextToolCall("Done. The response is now:\n\n```json\n{\"tasks\": [], \"next_cursor\": \"\"}\n```") {
		t.Fatal("an example response body is not a tool call")
	}
}
