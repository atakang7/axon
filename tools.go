package main

// tools.go — agent tool surface.
//
// Design contract (see memory: project_axon_tools_design.md, project_axon_tools_spec.md):
//
//   1. Single LLM, no subagents. Tools are plain functions.
//   2. Six tools: read, write, exec, search, archive, rearchive.
//      archive + rearchive live in memory.go (they own session memory state).
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
//   7. No mutation blocklist. The LLM decides what's destructive; loop-level
//      permission prompts handle dangerous actions, not tool-level gates.
//   8. Self-observation: misuse signals are recorded per session so the agent
//      loop can ghost a misused mode later. Tools record; they do not enforce.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Tool surface — public types and constants
// ---------------------------------------------------------------------------

type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Fn          func(json.RawMessage) (string, error)
}

const (
	toolRead    = "read"
	toolWrite   = "write"
	toolExec    = "exec"
	toolSearch  = "search"
	toolTask    = "task"
	toolPark    = "park"
	toolRecall  = "recall"
	toolForget  = "forget"
	toolRefresh = "refresh"
)

const (
	nextStepContinue = "continue"
	nextStepEnd      = "end"
)

// Mode constants. Required on read/write/search; one door per call.
const (
	readSkeleton = "skeleton"
	readSlice    = "slice"
	readFull     = "full"

	writeCreate     = "create"
	writeOverwrite  = "overwrite"
	writeReplaceStr = "replace_string"
	writeReplaceLn  = "replace_lines"
	writeInsertAt   = "insert_at_line"

	execRun    = "run"
	execVerify = "verify"

	searchLiteral = "literal"
	searchRegex   = "regex"
	searchTrace   = "trace"
)

func BuildTools(s *Session) []Tool {
	return []Tool{
		ReadTool(s),
		WriteTool(s),
		ExecTool(s),
		SearchTool(s),
		TaskTool(s),
		ParkTool(s),
		RecallTool(s),
		ForgetTool(s),
		RefreshTool(s),
	}
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

func enumSchema(desc string, values ...string) map[string]any {
	vs := make([]any, len(values))
	for i, v := range values {
		vs[i] = v
	}
	return map[string]any{"type": "string", "description": desc, "enum": vs}
}

// reasonField — required justification block on every tool call.
func reasonField() map[string]any {
	return strSchema("Why this call, and what you expect back. One sentence. Required: forces planning before paying the cost.")
}

func nextStepField() map[string]any {
	return enumSchema("What the runtime should do after this tool finishes. continue = send this tool result back to the LLM for another round. end = stop after recording this tool result and wait for the user.", nextStepContinue, nextStepEnd)
}

func shouldEndTurnAfterTool(name string, raw json.RawMessage) bool {
	switch name {
	case toolPark, toolRecall, toolForget, toolRefresh:
	default:
		return false
	}
	var p struct {
		NextStep string `json:"next_step"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return false
	}
	return strings.TrimSpace(p.NextStep) == nextStepEnd
}

// ---------------------------------------------------------------------------
// Shared utilities
// ---------------------------------------------------------------------------

func writeBytes(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

// limitBuf is a write-capped buffer used to bound tool output size.
type limitBuf struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitBuf) Write(p []byte) (int, error) {
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.truncated = true
		b.buf.Write(p[:remaining])
		return len(p), nil
	}
	return b.buf.Write(p)
}

// requireReason returns an error if the LLM omitted the justification field.
func requireReason(r string) error {
	if strings.TrimSpace(r) == "" {
		return fmt.Errorf("reason is required: state in one sentence why you are calling this tool")
	}
	return nil
}

// ---------------------------------------------------------------------------
// READ — single-file content. Three modes, conscious pick.
// ---------------------------------------------------------------------------

const readDescription = `Read one file. Pick mode by intent:
  - skeleton: function/type signatures with line numbers. ~10x cheaper than full. Use first for unfamiliar files.
  - slice: lines [offset, offset+limit). Use when you have line numbers (from a prior skeleton or search:trace).
  - full: entire file. Use only for small files or after skeleton confirmed you need everything.
Reading the same file repeatedly via small slices is the most expensive failure mode — pick the right mode the first time.`

func ReadTool(s *Session) Tool {
	limit := readLimit()
	return Tool{
		Name:        toolRead,
		Description: readDescription,
		Schema: obj("object", props{
			"path":   strSchema("Relative or absolute file path."),
			"mode":   enumSchema("skeleton | slice | full. Required.", readSkeleton, readSlice, readFull),
			"offset": intSchema("1-based start line. Required when mode=slice."),
			"limit":  intSchema(fmt.Sprintf("Lines to return. Required when mode=slice. Default %d if omitted.", limit)),
			"reason": reasonField(),
		}, []string{"path", "mode", "reason"}),
		Fn: func(raw json.RawMessage) (string, error) {
			var p struct {
				Path   string `json:"path"`
				Mode   string `json:"mode"`
				Offset int    `json:"offset"`
				Limit  int    `json:"limit"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Path) == "" {
				return "", fmt.Errorf("path is required")
			}
			abs := s.ResolvePath(p.Path)
			switch p.Mode {
			case readSkeleton:
				return readSkeletonMode(abs)
			case readSlice:
				if p.Offset < 1 {
					p.Offset = 1
				}
				if p.Limit <= 0 {
					p.Limit = readLimit()
				}
				return readSliceMode(abs, p.Offset, p.Limit)
			case readFull:
				return readFullMode(abs)
			default:
				return "", fmt.Errorf("mode is required: skeleton | slice | full")
			}
		},
	}
}

func readSliceMode(abs string, offset, limit int) (string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for line := 1; sc.Scan(); line++ {
		if line < offset {
			continue
		}
		out = append(out, fmt.Sprintf("%d\t%s", line, sc.Text()))
		if len(out) >= limit {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "[empty range]", nil
	}
	return strings.Join(out, "\n"), nil
}

func readFullMode(abs string) (string, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%d\t%s\n", i+1, line)
	}
	approx := len(data) / 4 // ~4 chars per token, rough
	header := fmt.Sprintf("[full read: ~%d tokens. consider mode=skeleton (~10x cheaper) or mode=slice if you need only part of this file]\n", approx)
	return header + strings.TrimRight(b.String(), "\n"), nil
}

// readSkeletonMode emits structural lines: imports, top-level decls, signatures.
// Heuristic — language-agnostic regex pass. Good enough for Go/JS/TS/Python.
// Tree-sitter upgrade is a future step.
var skeletonPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*(package|import|from|using|namespace)\b`),
	regexp.MustCompile(`^\s*(func|function|def|class|type|struct|interface|trait|enum|impl|module)\b`),
	regexp.MustCompile(`^\s*(public|private|protected|static|export|async|const|let|var)\s+(function|class|def|fn|struct|type|interface|enum)\b`),
	regexp.MustCompile(`^\s*(export\s+)?(default\s+)?(function|class|const|let|var|async)\b.*[({]`),
}

func readSkeletonMode(abs string) (string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for line := 1; sc.Scan(); line++ {
		text := sc.Text()
		for _, re := range skeletonPatterns {
			if re.MatchString(text) {
				out = append(out, fmt.Sprintf("%d\t%s", line, text))
				break
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "[skeleton found no signatures — file may be data, prose, or unsupported language. Try mode=slice or mode=full]", nil
	}
	header := "[skeleton: top-level declarations. Use mode=slice with these line numbers to read bodies]\n"
	return header + strings.Join(out, "\n"), nil
}

// ---------------------------------------------------------------------------
// WRITE — five modes. Each mode has a deterministic contract.
// ---------------------------------------------------------------------------

const writeDescription = `Write to a file. Pick mode by intent:
  - create: file must not exist. Writes content as the new file.
  - overwrite: replaces the entire file with content.
  - replace_string: replaces a single occurrence of old_str with content. old_str must match EXACTLY (whitespace included). If your output struggles with exact whitespace, use replace_lines instead.
  - replace_lines: replaces lines [start_line, end_line] with content. Deterministic — no string matching. Get line numbers from read.
  - insert_at_line: inserts content BEFORE start_line (1-based). The existing line at start_line shifts down.`

func WriteTool(s *Session) Tool {
	return Tool{
		Name:        toolWrite,
		Description: writeDescription,
		Schema: obj("object", props{
			"path":       strSchema("Relative or absolute file path."),
			"mode":       enumSchema("create | overwrite | replace_string | replace_lines | insert_at_line. Required.", writeCreate, writeOverwrite, writeReplaceStr, writeReplaceLn, writeInsertAt),
			"content":    strSchema("New content. Required for all modes."),
			"old_str":    strSchema("Exact text to replace. Required when mode=replace_string."),
			"start_line": intSchema("1-based start line. Required when mode=replace_lines or insert_at_line."),
			"end_line":   intSchema("1-based end line, inclusive. Required when mode=replace_lines."),
			"reason":     reasonField(),
		}, []string{"path", "mode", "content", "reason"}),
		Fn: func(raw json.RawMessage) (string, error) {
			var p struct {
				Path      string `json:"path"`
				Mode      string `json:"mode"`
				Content   string `json:"content"`
				OldStr    string `json:"old_str"`
				StartLine int    `json:"start_line"`
				EndLine   int    `json:"end_line"`
				Reason    string `json:"reason"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Path) == "" {
				return "", fmt.Errorf("path is required")
			}
			abs := s.ResolvePath(p.Path)
			switch p.Mode {
			case writeCreate:
				return writeCreateMode(s, abs, p.Content)
			case writeOverwrite:
				return writeOverwriteMode(s, abs, p.Content)
			case writeReplaceStr:
				if p.OldStr == "" {
					return "", fmt.Errorf("old_str is required for mode=replace_string (use overwrite if you mean to replace the whole file)")
				}
				return writeReplaceStringMode(s, abs, p.OldStr, p.Content)
			case writeReplaceLn:
				if p.StartLine < 1 || p.EndLine < p.StartLine {
					return "", fmt.Errorf("start_line >= 1 and end_line >= start_line are required for mode=replace_lines")
				}
				return writeReplaceLinesMode(s, abs, p.StartLine, p.EndLine, p.Content)
			case writeInsertAt:
				if p.StartLine < 1 {
					return "", fmt.Errorf("start_line >= 1 is required for mode=insert_at_line")
				}
				return writeInsertAtMode(s, abs, p.StartLine, p.Content)
			default:
				return "", fmt.Errorf("mode is required: create | overwrite | replace_string | replace_lines | insert_at_line")
			}
		},
	}
}

func writeCreateMode(s *Session, abs, content string) (string, error) {
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("file exists; use mode=overwrite or mode=replace_lines instead")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if dir := filepath.Dir(abs); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}
	s.RecordEdit(abs, "", content)
	return "created " + abs, writeBytes(abs, []byte(content))
}

func writeOverwriteMode(s *Session, abs, content string) (string, error) {
	before, _ := os.ReadFile(abs) // empty if absent — overwrite is permissive
	s.RecordEdit(abs, string(before), content)
	if dir := filepath.Dir(abs); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}
	return "overwrote " + abs, writeBytes(abs, []byte(content))
}

func writeReplaceStringMode(s *Session, abs, oldStr, newStr string) (string, error) {
	before, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	old := string(before)
	count := strings.Count(old, oldStr)
	if count == 0 {
		return "", fmt.Errorf("old_str not found — verify exact whitespace, or use mode=replace_lines for deterministic line-based edits")
	}
	if count > 1 {
		return "", fmt.Errorf("old_str matches %d times — provide more surrounding context to make it unique, or use mode=replace_lines", count)
	}
	after := strings.Replace(old, oldStr, newStr, 1)
	s.RecordEdit(abs, old, after)
	return "replaced 1 occurrence in " + abs, writeBytes(abs, []byte(after))
}

func writeReplaceLinesMode(s *Session, abs string, startLine, endLine int, content string) (string, error) {
	before, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	old := string(before)
	lines := strings.Split(old, "\n")
	if startLine > len(lines) {
		return "", fmt.Errorf("start_line %d is past end of file (%d lines)", startLine, len(lines))
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	head := lines[:startLine-1]
	tail := lines[endLine:]
	replacement := strings.Split(strings.TrimRight(content, "\n"), "\n")
	newLines := append(append(append([]string{}, head...), replacement...), tail...)
	after := strings.Join(newLines, "\n")
	s.RecordEdit(abs, old, after)
	return fmt.Sprintf("replaced lines %d-%d in %s", startLine, endLine, abs), writeBytes(abs, []byte(after))
}

func writeInsertAtMode(s *Session, abs string, startLine int, content string) (string, error) {
	before, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	old := string(before)
	lines := strings.Split(old, "\n")
	if startLine > len(lines)+1 {
		return "", fmt.Errorf("start_line %d is past end of file (%d lines)", startLine, len(lines))
	}
	head := lines[:startLine-1]
	tail := lines[startLine-1:]
	insert := strings.Split(strings.TrimRight(content, "\n"), "\n")
	newLines := append(append(append([]string{}, head...), insert...), tail...)
	after := strings.Join(newLines, "\n")
	s.RecordEdit(abs, old, after)
	return fmt.Sprintf("inserted at line %d in %s", startLine, abs), writeBytes(abs, []byte(after))
}

// ---------------------------------------------------------------------------
// SEARCH — multi-file content. Three modes.
// ---------------------------------------------------------------------------

const searchDescription = `Search across files. Pick mode by intent:
  - literal: exact string match. Fast, high precision. Use when locating an exact token.
  - regex: regex match. Same cost as literal, more flexible.
  - trace: symbol-aware. Give a function/type name; returns its definition, all callers, and all callees in a unified view. Trace replaces what would otherwise be 5+ separate searches and reads.`

func SearchTool(s *Session) Tool {
	return Tool{
		Name:        toolSearch,
		Description: searchDescription,
		Schema: obj("object", props{
			"query":  strSchema("Text, regex pattern, or symbol name (depending on mode)."),
			"mode":   enumSchema("literal | regex | trace. Required.", searchLiteral, searchRegex, searchTrace),
			"path":   strSchema("Optional search root. Default '.'."),
			"globs":  arr(strSchema("Optional rg glob filters, e.g. '*.go'.")),
			"reason": reasonField(),
		}, []string{"query", "mode", "reason"}),
		Fn: func(raw json.RawMessage) (string, error) {
			var p struct {
				Query  string   `json:"query"`
				Mode   string   `json:"mode"`
				Path   string   `json:"path"`
				Globs  []string `json:"globs"`
				Reason string   `json:"reason"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Query) == "" {
				return "", fmt.Errorf("query is required")
			}
			if strings.TrimSpace(p.Path) == "" {
				p.Path = "."
			}
			switch p.Mode {
			case searchLiteral:
				return runRipgrep(s, p.Query, p.Path, p.Globs, true)
			case searchRegex:
				return runRipgrep(s, p.Query, p.Path, p.Globs, false)
			case searchTrace:
				return runTrace(s, p.Query, p.Path, p.Globs)
			default:
				return "", fmt.Errorf("mode is required: literal | regex | trace")
			}
		},
	}
}

func runRipgrep(s *Session, query, path string, globs []string, literal bool) (string, error) {
	args := []string{"-n", "--no-heading", "--color", "never", "-g", "!.git", "--hidden", "--ignore-case"}
	if literal {
		args = append(args, "--fixed-strings")
	}
	for _, g := range globs {
		if g = strings.TrimSpace(g); g != "" {
			args = append(args, "-g", g)
		}
	}
	args = append(args, query, path)

	cmd := exec.Command("rg", args...)
	if s.Cwd != "" {
		cmd.Dir = s.Cwd
	}
	buf := &limitBuf{limit: searchOutputLimit()}
	cmd.Stdout = buf
	cmd.Stderr = buf
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return fmt.Sprintf("query: %s\npath: %s\nno matches", query, path), nil
		}
		if strings.Contains(err.Error(), "executable file not found") {
			return "", fmt.Errorf("search requires rg in PATH")
		}
		return "", err
	}

	out := strings.TrimRight(buf.buf.String(), "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "query: %s\npath: %s\n", query, path)
	if out == "" {
		b.WriteString("no matches")
	} else {
		b.WriteString(out)
	}
	if buf.truncated {
		b.WriteString("\n[output truncated — narrow with globs or refine query]")
	}
	return b.String(), nil
}

// runTrace: regex-based symbol trace. Finds definitions, callers, and callees.
// Heuristic. Good enough for ~80% of cases. Tree-sitter upgrade is future work.
func runTrace(s *Session, symbol, path string, globs []string) (string, error) {
	defPattern := fmt.Sprintf(`(func|function|def|class|type|struct|interface)\s+%s\b`, regexp.QuoteMeta(symbol))
	callPattern := fmt.Sprintf(`\b%s\s*\(`, regexp.QuoteMeta(symbol))

	defs, err := rgCollect(s, defPattern, path, globs)
	if err != nil {
		return "", err
	}
	calls, err := rgCollect(s, callPattern, path, globs)
	if err != nil {
		return "", err
	}

	// Partition calls into "in the definition file at the def line" (skip), elsewhere (callers).
	defLocs := map[string]bool{}
	for _, d := range defs {
		defLocs[d.file+":"+fmt.Sprintf("%d", d.line)] = true
	}
	var callers []rgHit
	for _, c := range calls {
		if defLocs[c.file+":"+fmt.Sprintf("%d", c.line)] {
			continue
		}
		callers = append(callers, c)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "SYMBOL: %s\n\n", symbol)
	if len(defs) == 0 {
		b.WriteString("DEFINED: <not found>\n")
	} else {
		b.WriteString("DEFINED:\n")
		for _, d := range defs {
			fmt.Fprintf(&b, "  %s:%d  %s\n", d.file, d.line, strings.TrimSpace(d.text))
		}
	}
	b.WriteString("\nCALLED FROM:\n")
	if len(callers) == 0 {
		b.WriteString("  <no callers found>\n")
	} else {
		for _, c := range callers {
			fmt.Fprintf(&b, "  %s:%d  %s\n", c.file, c.line, strings.TrimSpace(c.text))
		}
	}
	b.WriteString("\n[trace: regex heuristic. May miss method calls on receivers or shadowed names. Use search:literal/regex for verification if needed.]")
	return b.String(), nil
}

type rgHit struct {
	file string
	line int
	text string
}

func rgCollect(s *Session, pattern, path string, globs []string) ([]rgHit, error) {
	args := []string{"-n", "--no-heading", "--color", "never", "-g", "!.git"}
	for _, g := range globs {
		if g = strings.TrimSpace(g); g != "" {
			args = append(args, "-g", g)
		}
	}
	args = append(args, pattern, path)
	cmd := exec.Command("rg", args...)
	if s.Cwd != "" {
		cmd.Dir = s.Cwd
	}
	buf := &limitBuf{limit: searchOutputLimit()}
	cmd.Stdout = buf
	cmd.Stderr = buf
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		if strings.Contains(err.Error(), "executable file not found") {
			return nil, fmt.Errorf("trace requires rg in PATH")
		}
		return nil, err
	}
	var hits []rgHit
	for _, ln := range strings.Split(strings.TrimRight(buf.buf.String(), "\n"), "\n") {
		if ln == "" {
			continue
		}
		// rg format: file:line:text
		parts := strings.SplitN(ln, ":", 3)
		if len(parts) < 3 {
			continue
		}
		var lineNum int
		fmt.Sscanf(parts[1], "%d", &lineNum)
		hits = append(hits, rgHit{file: parts[0], line: lineNum, text: parts[2]})
	}
	return hits, nil
}

// ---------------------------------------------------------------------------
// EXEC — non-interactive shell command. LLM controls tail size.
// ---------------------------------------------------------------------------

const execDescription = `Run a shell command. Two modes:
  - run: execute an arbitrary non-interactive command. command and tail_lines are required.
  - verify: auto-detect and run the project's build/type-check command (go build, tsc, cargo check, etc.) from the cwd. No command needed. Use this after every write to catch errors fast.`

func detectVerifyCommand(dir string) (string, error) {
	for _, marker := range []struct {
		file string
		cmd  string
	}{
		{"go.mod", "go build ./..."},
		{"Cargo.toml", "cargo check"},
		{"tsconfig.json", "tsc --noEmit"},
		{"package.json", "npm run build --if-present"},
		{"Makefile", "make"},
		{"pyproject.toml", "python -m py_compile $(find . -name '*.py' | head -20)"},
		{"setup.py", "python -m py_compile $(find . -name '*.py' | head -20)"},
	} {
		if _, err := os.Stat(filepath.Join(dir, marker.file)); err == nil {
			return marker.cmd, nil
		}
	}
	return "", fmt.Errorf("could not detect build/check command: no go.mod, Cargo.toml, tsconfig.json, package.json, or Makefile found in %s", dir)
}

func ExecTool(s *Session) Tool {
	timeout := execTimeoutSeconds()
	return Tool{
		Name:        toolExec,
		Description: execDescription,
		Schema: obj("object", props{
			"mode":             enumSchema("run | verify. Required.", execRun, execVerify),
			"command":          strSchema("Shell command. Required for mode=run."),
			"tail_lines":       intSchema("Last N lines to keep. Required for mode=run; defaults to 50 for mode=verify."),
			"expected_outcome": strSchema("What success looks like. Optional but enables structured failure diagnosis."),
			"dir":              strSchema("Optional working directory override."),
			"timeout_seconds":  intSchema(fmt.Sprintf("Default %d.", timeout)),
			"reason":           reasonField(),
		}, []string{"mode", "reason"}),
		Fn: func(raw json.RawMessage) (string, error) {
			var p struct {
				Mode            string `json:"mode"`
				Command         string `json:"command"`
				TailLines       int    `json:"tail_lines"`
				ExpectedOutcome string `json:"expected_outcome"`
				Dir             string `json:"dir"`
				TimeoutSeconds  int    `json:"timeout_seconds"`
				Reason          string `json:"reason"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}

			resolvedDir := s.Cwd
			if strings.TrimSpace(p.Dir) != "" {
				resolvedDir = s.ResolvePath(p.Dir)
			}

			switch p.Mode {
			case execVerify:
				cmd, err := detectVerifyCommand(resolvedDir)
				if err != nil {
					return "", err
				}
				p.Command = cmd
				if p.TailLines <= 0 {
					p.TailLines = 50
				}
				if p.ExpectedOutcome == "" {
					p.ExpectedOutcome = "no errors"
				}
			case execRun:
				if strings.TrimSpace(p.Command) == "" {
					return "", fmt.Errorf("command is required for mode=run")
				}
				if p.TailLines <= 0 {
					return "", fmt.Errorf("tail_lines is required and must be > 0 for mode=run")
				}
			default:
				return "", fmt.Errorf("mode is required: run | verify")
			}
			if p.TimeoutSeconds <= 0 {
				p.TimeoutSeconds = timeout
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.TimeoutSeconds)*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, "sh", "-lc", p.Command)
			if resolvedDir != "" {
				cmd.Dir = resolvedDir
			}
			if dn, err := os.Open(os.DevNull); err == nil {
				cmd.Stdin = dn
				defer dn.Close()
			}

			buf := &limitBuf{limit: execOutputLimit()}
			cmd.Stdout = buf
			cmd.Stderr = buf

			runErr := cmd.Run()
			code := 0
			note := ""
			if runErr != nil {
				if ctx.Err() == context.DeadlineExceeded {
					code = -1
					note = "timed out"
				} else if exitErr, ok := runErr.(*exec.ExitError); ok {
					code = exitErr.ExitCode()
				} else {
					return "", runErr
				}
			}

			tailed, hidden := tailN(buf.buf.String(), p.TailLines)
			return formatExec(p.Command, cmd.Dir, code, p.ExpectedOutcome, tailed, hidden, buf.truncated, note), nil
		},
	}
}

func tailN(s string, n int) (string, int) {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s, 0
	}
	return strings.Join(lines[len(lines)-n:], "\n"), len(lines) - n
}

func formatExec(command, dir string, code int, expected, out string, hidden int, truncated bool, note string) string {
	var b strings.Builder
	b.WriteString("$ " + command + "\n")
	if dir != "" {
		b.WriteString("dir: " + dir + "\n")
	}
	if expected != "" {
		b.WriteString("expected: " + expected + "\n")
	}
	fmt.Fprintf(&b, "exit_code: %d", code)
	if note != "" {
		b.WriteString(" (" + note + ")")
	}
	b.WriteString("\n")
	if hidden > 0 {
		fmt.Fprintf(&b, "[%d earlier lines hidden]\n", hidden)
	}
	if strings.TrimSpace(out) != "" {
		b.WriteString(strings.TrimRight(out, "\n"))
	}
	if truncated {
		b.WriteString("\n[output truncated at byte limit]")
	}
	return b.String()
}
