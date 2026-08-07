// Package axon is the vocabulary an embedder builds with: the types you
// construct, implement or inspect when you use the runtime in
// github.com/atakang7/axon/agent.
//
// It exists as its own package for one reason. Everything below the public
// API lives under internal/, which makes it unreachable — and also makes it
// undocumentable: `go doc agent.Tool` on an alias into an internal package
// prints the alias and stops, so a caller could not discover that a Tool has a
// Name, a Schema or an Fn without reading the source. The types you have to
// build belong somewhere you can read about them.
//
// This package is a leaf. It holds no behaviour, imports nothing from this
// module, and every layer above may import it.
//
//	model, err := axon.Model(...)   // implement, or use agent.OpenAI
//	tool := axon.Tool{...}          // construct
//	agent.New(agent.Config{...})    // drive
package axon

import (
	"context"
	"encoding/json"
)

// ---------------------------------------------------------------------------
// The model
// ---------------------------------------------------------------------------

// Model is an LLM the runtime can talk to. It is the whole contract.
//
// Implement it to reach a provider that is not OpenAI-compatible, to route
// through your own gateway, or to supply a deterministic fake so an agent's
// turn loop can be driven in tests with no network and no API key.
// agent.OpenAI returns the implementation that ships.
type Model interface {
	Complete(ctx context.Context, req Request) (*Msg, error)
}

// Request is one completion.
type Request struct {
	// Messages is the conversation as the model should see it.
	Messages []Msg
	// Tools the model may call. Empty means none.
	Tools []ToolSpec
	// MaxTokens caps this one reply. Zero means the model's own default.
	// It is per-request because callers want different budgets: an agent turn
	// may need thousands of tokens, while the pruner needs one line of JSON.
	MaxTokens int
	// Stream receives output as it arrives. The zero value discards it.
	Stream Stream
}

// Stream receives incremental output during a completion. Every field is
// optional; a nil func is simply not called.
//
// Callbacks run synchronously on the goroutine reading the response, in
// arrival order, and must not block. Anything slow — a network write to a
// browser, a disk flush — has to be handed to a buffered channel and done
// elsewhere: blocking here stops the read, and a stall long enough to trip the
// idle timeout fails the whole completion.
//
// Reasoning is separate from Token because reasoning models emit a long
// thinking block before any content, and a caller usually wants to render the
// two differently. ToolArgs exists because some providers buffer tool-call
// arguments to end-of-message rather than streaming them, so a UI watching
// only Token can look frozen during a perfectly healthy stream.
type Stream struct {
	Token     func(text string)
	Reasoning func(text string)
	ToolArgs  func(name, delta string)
}

// Provider is one endpoint: base URL, model name, credentials, and any
// provider-specific routing options, which are forwarded verbatim as the
// request's "provider" field.
//
// How you decide which provider to use — a config file, flags, an environment
// cascade — is yours to choose. The runtime only wants the resolved answer.
type Provider struct {
	// Name labels the provider, e.g. "openai" or "openrouter".
	Name string
	// BaseURL is the API root. "/v1" is appended when absent. Required.
	BaseURL string
	// Model is the model identifier the provider expects. Required.
	Model string
	// APIKey is sent as a bearer token when set.
	APIKey string
	// Extra is provider-specific routing JSON, passed through untouched.
	Extra json.RawMessage
}

// ---------------------------------------------------------------------------
// Conversation
// ---------------------------------------------------------------------------

// Msg is one entry in the conversation. A session's message log is
// append-only: nothing here is rewritten after it is recorded.
//
// Parked is set by the pruner. It means the projection sent to the model
// carries a one-line breadcrumb in place of Content; Content itself is never
// modified, so the full history survives for audit.
type Msg struct {
	Role        string     `json:"role"`
	Content     string     `json:"content,omitempty"`
	ToolCalls   []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID  string     `json:"tool_call_id,omitempty"`
	ToolName    string     `json:"tool_name,omitempty"`
	ID          string     `json:"id,omitempty"`
	Parked      bool       `json:"parked,omitempty"`
	ParkSummary string     `json:"park_summary,omitempty"`
	ParkReason  string     `json:"park_reason,omitempty"`
}

// ToolCall is the model's request to invoke one tool. Arguments is raw JSON
// as the model emitted it, which is not guaranteed to match the schema.
type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

// Tool is a named function the model can call. This is the extension surface:
// put your own in agent.Config.Tools.
//
//	deploy := axon.Tool{
//	    Name:        "deploy",
//	    Description: "Deploy a service to staging.",
//	    Schema: map[string]any{
//	        "type":       "object",
//	        "properties": map[string]any{"service": map[string]any{"type": "string"}},
//	        "required":   []string{"service"},
//	    },
//	    Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
//	        var p struct{ Service string }
//	        if err := json.Unmarshal(args, &p); err != nil {
//	            return "", err
//	        }
//	        return deployTo(ctx, p.Service)
//	    },
//	}
//
// The string Fn returns goes into the conversation as the tool's result, so it
// is written for the model to read. Returning an error is not fatal: the error
// text becomes the result and the model gets to react to it, which is usually
// what you want for a failure it could recover from.
//
// Name, Schema and Fn are required. agent.New rejects a Tool missing any of
// them with agent.ErrInvalidTool rather than letting the turn loop discover it:
// a nil Fn would otherwise panic mid-turn, after the model had already
// committed to the call.
type Tool struct {
	// Name is what the model calls. Required. It must not collide with a
	// built-in the agent still has (read, write, exec, search, task,
	// bash_output, kill_shell) — a built-in removed via Config.ExcludeBuiltins
	// frees its name.
	Name string
	// Description tells the model when to reach for this tool. Optional, but a
	// tool the model cannot tell apart from the others will not get called.
	Description string
	// Schema is the JSON Schema for Fn's arguments. Required; use
	// map[string]any{"type": "object"} for a tool that takes none. It is not
	// optional because providers disagree about what a null parameter schema
	// means, and the disagreement surfaces as a rejected request.
	Schema map[string]any
	// Fn runs the tool. The context is turn-scoped: it is cancelled when the
	// turn is interrupted, so long-running work should honour it.
	Fn func(ctx context.Context, args json.RawMessage) (string, error)
}

// ToolSpec is a tool as the model sees it: name, description, schema, and
// deliberately no implementation.
//
// This is the contract that keeps the model layer independent of the execution
// layer. The runtime projects each Tool down to a ToolSpec at the call
// boundary, so a Model implementation can never reach a tool's behaviour —
// only its shape.
type ToolSpec struct {
	Name        string
	Description string
	Schema      map[string]any
}
