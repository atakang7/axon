package agent

// tools.go — agent tool surface.
//
// Design contract (see memory: project_axon_tools_design.md, project_axon_tools_spec.md):
//
//   1. Single LLM, no subagents. Tools are plain functions.
//   2. Tools: read, write, exec, search, bash_output, kill_shell, task.
//      task lives in memory.go (it owns session task state).
//   3. Every tool takes a required `reason` field — the LLM must articulate
//      intent before paying the cost. The reason is recorded for self-observation,
//      not enforced by length or content.
//   4. Every tool's `mode` is required with no default. The LLM picks consciously.
//      "One door" steering, never amputation: every mode stays available at
//      every hygiene level. Friction and metadata scale, capability does not.
//   5. Tool descriptions teach the cost model in plain terms ("full read is ~10x
//      skeleton", "tool-call loops resend full context"). Reality, not nagging.
//   6. Output is structured and traceable. Search/trace returns a unified "bingo"
//      view across files. Exec failures return diagnostics, not raw dumps.
//   7. No mutation blocklist and no built-in approval prompt today. The LLM
//      decides what's destructive. Hard caps that DO exist: per-call exec
//      timeout (capped by AXON_EXEC_MAX_TIMEOUT_SECONDS), tail-line cap
//      (AXON_EXEC_MAX_TAIL_LINES), output byte caps on exec/search, full-read
//      size cap (AXON_READ_MAX_BYTES), and binary-file refusal on read. Tool
//      execution is bound to the turn context so Ctrl-C kills the running
//      command's process group. A user-facing approval/sandbox layer is a
//      future addition, not present in this build — do not assume it exists.
//   8. Atomicity: all writes go through writeBytesRaw (tmp + rename, mode
//      preserved). The wrapper writeBytes runs the optional formatter; /undo
//      uses writeBytesRaw directly so it is byte-exact and never reformats.

import (
	"context"
	"encoding/json"
	"fmt"
)

// ---------------------------------------------------------------------------
// Tool surface — public types and constants
// ---------------------------------------------------------------------------

type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	// Fn receives the turn-scoped context so long-running tools (foreground
	// exec, search) cancel cleanly when the user hits Ctrl-C or the parent
	// context fires. Tools that don't need cancellation may ignore ctx.
	Fn func(ctx context.Context, args json.RawMessage) (string, error)
}

const (
	toolRead       = "read"
	toolWrite      = "write"
	toolExec       = "exec"
	toolBashOutput = "bash_output"
	toolKillShell  = "kill_shell"
	toolSearch     = "search"
	toolTask       = "task"
)

// Mode constants. Required on read/write/search; one door per call.
const (
	readSkeleton = "skeleton"
	readSlice    = "slice"
	readFull     = "full"

	writeSave       = "save"
	writeReplaceStr = "replace_string"
	writeReplaceLn  = "replace_lines"
	writeInsertAt   = "insert_at_line"

	execRun    = "run"
	execVerify = "verify"

	searchLiteral = "literal"
	searchRegex   = "regex"
	searchTrace   = "trace"
)

// BuildTools assembles the tool set for one agent. Built-ins ("hands and
// legs": read, write, exec, search, task, bash_output, kill_shell) are
// always added unless the agent config explicitly disables them. Custom
// tools declared in cfg.Tools are appended.
//
// cfg may be nil — in that case every built-in is enabled and no custom
// tools are added, matching the pre-config default behavior.
//
// Returns an error only if a custom tool fails to build (bad template,
// unknown type). Built-in construction is infallible.
func BuildTools(s *Session, cfg *AgentConfig) ([]Tool, error) {
	type builtin struct {
		name string
		make func(*Session) Tool
	}
	builtins := []builtin{
		{toolRead, ReadTool},
		{toolWrite, WriteTool},
		{toolExec, ExecTool},
		{toolBashOutput, BashOutputTool},
		{toolKillShell, KillShellTool},
		{toolSearch, SearchTool},
		{toolTask, TaskTool},
	}
	var tools []Tool
	for _, b := range builtins {
		if cfg != nil && cfg.DisabledBuiltin(b.name) {
			continue
		}
		tools = append(tools, b.make(s))
	}
	if cfg != nil {
		for _, tc := range cfg.Tools {
			t, err := buildCustomTool(tc)
			if err != nil {
				return nil, fmt.Errorf("custom tool %q: %w", tc.Name, err)
			}
			tools = append(tools, t)
		}
	}
	return tools, nil
}

// ---------------------------------------------------------------------------
// Schema helpers
// ---------------------------------------------------------------------------

type props = map[string]map[string]any

func obj(typ string, p props, required []string) map[string]any {
	m := map[string]any{"type": typ, "additionalProperties": false}
	if p != nil {
		mp := map[string]any{}
		for k, v := range p {
			mp[k] = v
		}
		m["properties"] = mp
	}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func arr(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func strSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intSchema(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func boolSchema(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func enumSchema(desc string, values ...string) map[string]any {
	vs := make([]any, len(values))
	for i, v := range values {
		vs[i] = v
	}
	return map[string]any{"type": "string", "description": desc, "enum": vs}
}

// reasonField — required justification block on every tool call.
func reasonField() map[string]any {
	return strSchema("Why this call SERVES THE CURRENT TASK STEP and HYPOTHESIS, what you expect the result to confirm or falsify, and what you will do next based on each possible outcome. Not a description of the call ('read file X to see contents') — a justification tied to the plan ('read X to verify the unconditional ID stamp at line 283 is the cause; if confirmed, advance to fix; if not, replan'). One or two sentences. The reason exists to force you to know why this call earns its cost.")
}
