// Package tools implements the agent's hands and legs: reading and writing
// files, running commands, searching a tree, and tracking a task plan.
//
// BOUNDARY RULE: tools may import session, llm and config. It must never
// import agent — the runtime depends on the tools, never the reverse.
//
// Tools do not receive a *session.Session. Each one takes the narrowest
// interface it actually needs (Workspace, Plan), declared here on the
// consumer side, which Session satisfies implicitly. That is what makes a
// tool independently testable: a fake Workspace is four lines, and a tool
// physically cannot reach conversation state that is none of its business.
package tools

// tools.go — tool surface, capability interfaces, schema helpers.
//
// Design contract (see memory: project_axon_tools_design.md, project_axon_tools_spec.md):
//
//   1. Single LLM, no subagents. Tools are plain functions.
//   2. Tools: read, write, exec, search, bash_output, kill_shell, task.
//   3. Tools take no `reason` field. Earlier builds required a justification
//      string on every call; it was dropped to cut per-call latency and token
//      cost. The model's own reasoning trace is the audit log.
//   4. `mode` is required on write and task (modes take different params and
//      have no safe default). On read/exec/search, `mode` is optional with a
//      sensible default (slice, run, literal) so the cheap common case is one
//      argument away. Every mode stays reachable; defaults only set the
//      default door.
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
//   8. Atomicity: all writes go through WriteFileAtomic (tmp + rename, mode
//      preserved), so a write is byte-exact and /undo restores exactly what
//      was there. The runtime does not format what it writes: bundling a
//      dispatch table of twenty third-party formatter binaries was unbounded
//      maintenance debt for a capability the agent already has — it can run
//      gofmt through exec like anyone else.

import (
	"fmt"
	"strings"

	axon "github.com/atakang7/axon"
	"github.com/atakang7/axon/internal/session"
)

// ---------------------------------------------------------------------------
// Capabilities
//
// The narrow interfaces tools depend on, declared here because this is the
// consuming side. *session.Session satisfies both; so does a four-line fake.
// ---------------------------------------------------------------------------

// Workspace is the directory a tool operates in, plus the ledger it records
// mutations to so they can be undone. Read, write, search and exec take this
// and nothing else — none of them can see the conversation.
type Workspace interface {
	// Dir is the absolute working directory commands run in.
	Dir() string
	// ResolvePath turns a possibly-relative path into an absolute one,
	// interpreted against Dir.
	ResolvePath(path string) string
	// RecordEdit stores a before/after pair so the write can be reverted.
	RecordEdit(path, before, after string)
}

// Plan is the task-tracking surface the task tool drives.
//
// The tool mutates the plan and is told what to do next; it cannot enumerate
// or re-read the plan. AdvanceTask returns the next step precisely so the tool
// can tell the model where it now is without gaining a reader for the whole
// structure. Rendering the plan into the prompt stays the runtime's job, so
// there is exactly one place that decides how a plan is presented.
type Plan interface {
	RegisterTask(goal string, steps []session.TaskStep) error
	AdvanceTask() (string, error)
	ReplanTask(goal string, steps []session.TaskStep) error
}

// ---------------------------------------------------------------------------
// Tool surface — public types and constants
// ---------------------------------------------------------------------------

type Tool = axon.Tool

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

// Catalog lists the built-in tools for the system prompt. Terse on purpose:
// the full per-mode documentation lives in each tool's Description, which the
// provider already sees through the tool schema, so repeating it here would
// pay for the same tokens twice on every single call.
//
// It lives in this package, rather than in the prompt builder, so that the
// names and the blurbs cannot drift apart from the tools they describe.
func Catalog() string {
	rows := []struct{ name, blurb string }{
		{toolRead, "Read files (skeleton / slice / full)."},
		{toolWrite, "Write files (create / overwrite / replace / insert)."},
		{toolExec, "Execute commands (run / verify; set run_in_background=true for servers and watchers)."},
		{toolBashOutput, "Read new output from a background shell (delta only)."},
		{toolKillShell, "Stop a background shell. Always clean up servers you started."},
		{toolSearch, "Search (literal / regex / trace)."},
		{toolTask, "Register a task objective."},
	}
	var b strings.Builder
	b.WriteString("# BUILT-IN TOOLS\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "\n%q — %s", r.name, r.blurb)
	}
	return b.String()
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
