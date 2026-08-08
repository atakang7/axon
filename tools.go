package axon

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Capabilities
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
	// RecordEdit stores the pre-edit contents of path so the write can be
	// reverted. The post-edit contents are not passed: reverting means writing
	// before back, and the file on disk already holds the new version.
	RecordEdit(path, before string)
}

// Plan is the task-tracking surface the task tool drives.
//
// The tool mutates the plan and is told what to do next; it cannot enumerate
// or re-read the plan. AdvanceTask returns the next step precisely so the tool
// can tell the model where it now is without gaining a reader for the whole
// structure. Rendering the plan into the prompt stays the runtime's job, so
// there is exactly one place that decides how a plan is presented.
type Plan interface {
	RegisterTask(goal string, steps []TaskStep) error
	AdvanceTask() (string, error)
	ReplanTask(goal string, steps []TaskStep) error
}

// ---------------------------------------------------------------------------
// Tool surface — public types and constants
// ---------------------------------------------------------------------------

const (
	toolRead       = "read"
	toolWrite      = "write"
	toolExec       = "exec"
	toolBashOutput = "bash_output"
	toolKillShell  = "kill_shell"
	toolSearch     = "search"
	toolTask       = "task"
)

// Mode constants. Required on write; optional with a default on read and
// search. One door per call.
const (
	readSlice = "slice"
	readFull  = "full"

	writeSave       = "save"
	writeReplaceStr = "replace_string"
	writeReplaceLn  = "replace_lines"
	writeInsertAt   = "insert_at_line"

	searchLiteral = "literal"
	searchRegex   = "regex"
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
		{toolRead, "Read files (slice / full) and list directories."},
		{toolWrite, "Write files (create / overwrite / replace / insert)."},
		{toolExec, "Execute commands (set run_in_background=true for servers and watchers)."},
		{toolBashOutput, "Read new output from a background shell (delta only)."},
		{toolKillShell, "Stop a background shell. Always clean up servers you started."},
		{toolSearch, "Search file contents (literal / regex)."},
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
