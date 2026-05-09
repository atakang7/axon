package main

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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
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

func BuildTools(s *Session) []Tool {
	return []Tool{
		ReadTool(s),
		WriteTool(s),
		ExecTool(s),
		BashOutputTool(s),
		KillShellTool(s),
		SearchTool(s),
		TaskTool(s),
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

// ---------------------------------------------------------------------------
// Shared utilities
// ---------------------------------------------------------------------------

// writeBytesRaw writes atomically: tmp file in the same dir, then rename. The
// rename is atomic on POSIX, so a crash mid-write never leaves a half-written
// file at `path` — readers see either the old contents or the new, never a
// truncated mix. Same-dir tmp matters: cross-filesystem renames degrade to
// copy+unlink and lose the atomicity guarantee.
//
// File mode handling: if the destination already exists, its mode is preserved
// (executable bits, group-readable scripts, etc.). New files default to 0644.
// No formatter runs here — callers that want formatting use writeBytes.
func writeBytesRaw(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".axon-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// writeBytes writes atomically and then runs the language-appropriate
// formatter best-effort. Use this from edit tools. Use writeBytesRaw from
// /undo and any other path that must be byte-exact.
func writeBytes(path string, data []byte) error {
	if err := writeBytesRaw(path, data); err != nil {
		return err
	}
	formatPath(path)
	return nil
}

// formatPath runs the appropriate formatter for the file's extension.
// Best-effort: any error (missing tool, parse failure, timeout) is swallowed —
// formatting must never break a successful write. The model is allowed to emit
// non-indented code for whitespace-insensitive languages and rely on this hook.
func formatPath(path string) {
	ext := strings.ToLower(filepath.Ext(path))
	cmd, args, ok := formatterFor(ext, path)
	if !ok {
		return
	}
	resolved, err := resolveFormatter(cmd)
	if err != nil {
		return
	}
	cmd = resolved
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, cmd, args...)
	// dprint resolves file paths against cwd, so run it from the file's
	// directory and pass the basename. gofmt etc. don't care.
	if strings.HasSuffix(cmd, "dprint") {
		c.Dir = filepath.Dir(path)
		// Replace the final arg (the absolute path) with just the basename.
		if n := len(args); n > 0 {
			args[n-1] = filepath.Base(path)
			c.Args = append([]string{cmd}, args...)
		}
	}
	_ = c.Run()
}

// resolveFormatter looks up a formatter binary on PATH, falling back to known
// install locations (e.g. dprint's default ~/.dprint/bin) so agents work even
// when the user hasn't shell-rc'd the install path.
func resolveFormatter(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, p := range []string{
		filepath.Join(home, ".dprint", "bin", name),
		filepath.Join(home, ".cargo", "bin", name),
		filepath.Join(home, ".local", "bin", name),
	} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("formatter %q not found", name)
}

// formatterFor maps a file extension to (binary, args). Returns ok=false when
// no formatter is configured for the extension.
//
// dprint covers: TS/JS/JSX/TSX, JSON, MD, TOML, YAML, Python (via ruff plugin),
// CSS, HTML, Dockerfile. Native binaries handle the rest. Order matters:
// native formatters take priority where they're idiomatic (gofmt for Go,
// rustfmt for Rust) so the result matches what the project's CI expects.
func formatterFor(ext, path string) (string, []string, bool) {
	switch ext {
	case ".go":
		return "gofmt", []string{"-w", path}, true
	case ".rs":
		return "rustfmt", []string{path}, true
	case ".sh", ".bash":
		return "shfmt", []string{"-w", path}, true
	case ".c", ".h", ".cpp", ".hpp", ".cc", ".cxx":
		return "clang-format", []string{"-i", path}, true
	case ".java":
		return "google-java-format", []string{"-i", path}, true
	case ".zig":
		return "zig", []string{"fmt", path}, true
	case ".kt", ".kts":
		return "ktlint", []string{"-F", path}, true
	case ".swift":
		return "swift-format", []string{"-i", path}, true
	case ".rb":
		return "rubocop", []string{"-a", "--no-color", path}, true
	case ".php":
		return "php-cs-fixer", []string{"fix", path}, true
	case ".scala", ".sbt":
		return "scalafmt", []string{path}, true
	case ".lua":
		return "stylua", []string{path}, true
	case ".dart":
		return "dart", []string{"format", path}, true
	case ".ex", ".exs":
		return "mix", []string{"format", path}, true
	case ".nix":
		return "nixpkgs-fmt", []string{path}, true
	case ".proto":
		return "buf", []string{"format", "-w", path}, true
	case ".sql":
		return "sqlfluff", []string{"fix", "--disable-progress-bar", path}, true
	case ".tf", ".tfvars":
		return "terraform", []string{"fmt", path}, true
	case ".xml":
		return "xmllint", []string{"--format", "--output", path, path}, true
	case ".py", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs",
		".json", ".jsonc", ".md", ".markdown", ".toml",
		".yaml", ".yml", ".css", ".scss", ".html", ".htm":
		return "dprint", []string{"fmt", "--config", dprintConfigPath(), path}, true
	}
	return "", nil, false
}

// dprintConfigPath returns the path to the agent's dprint config, creating it
// on first use. We keep one config under the user's config dir so dprint has
// a stable place to find plugins regardless of cwd.
func dprintConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "dprint.json"
	}
	dir := filepath.Join(home, ".config", "dprint")
	path := filepath.Join(dir, "dprint.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(path, []byte(defaultDprintConfig), 0644)
	return path
}

const defaultDprintConfig = `{
  "typescript": {},
  "json": {},
  "markdown": {},
  "toml": {},
  "yaml": {},
  "ruff": {},
  "plugins": [
    "https://plugins.dprint.dev/typescript-0.95.15.wasm",
    "https://plugins.dprint.dev/json-0.21.3.wasm",
    "https://plugins.dprint.dev/markdown-0.21.1.wasm",
    "https://plugins.dprint.dev/toml-0.7.0.wasm",
    "https://plugins.dprint.dev/g-plane/pretty_yaml-v0.6.0.wasm",
    "https://plugins.dprint.dev/ruff-0.7.11.wasm"
  ]
}
`

// binaryFileRefusal sniffs the first 8KB of path for binary indicators. NUL
// bytes are the strong signal (compiled executables, archives, images). We
// also flag content that is not valid UTF-8 and has a high ratio of control
// bytes (excluding tab/CR/LF) — that catches binary formats that happen to
// have no NULs in their header, like some compressed streams.
//
// Refuse up front with a one-line message that names size, so the model can
// decide whether to use a deliberate tool (strings, hexdump via exec) instead.
func binaryFileRefusal(abs string) (string, bool) {
	f, err := os.Open(abs)
	if err != nil {
		return "", false
	}
	defer f.Close()
	var buf [8192]byte
	n, _ := f.Read(buf[:])
	if n == 0 {
		return "", false
	}
	sample := buf[:n]
	hasNUL := false
	ctrl := 0
	for _, b := range sample {
		switch {
		case b == 0:
			hasNUL = true
		case b < 0x09, b == 0x0B, b == 0x0C, (b > 0x0D && b < 0x20), b == 0x7F:
			ctrl++
		}
	}
	// Decision tree:
	//   NUL byte                                → binary (strong signal).
	//   ctrl-byte ratio > 10% of sample         → binary. Bytes like
	//       0x01..0x08 are technically valid single-byte ASCII per
	//       utf8.Valid, so we cannot rely on UTF-8 validity alone — a
	//       sample full of 0x01..0x08 still passes utf8.Valid but is
	//       obviously not text.
	//   invalid UTF-8 AND any ctrl bytes        → binary. Catches binary
	//       streams that happen to be sub-10% control but are clearly not
	//       text encodings.
	// Files that are valid UTF-8 with no control bytes — including Latin-1
	// content that happens to also be valid UTF-8 — pass through.
	binary := hasNUL || ctrl*100 > n*10 || (!utf8.Valid(sample) && ctrl > 0)
	if !binary {
		return "", false
	}
	fi, _ := f.Stat()
	size := int64(-1)
	if fi != nil {
		size = fi.Size()
	}
	return fmt.Sprintf("[binary file refused: %s (%d bytes). Loading binary content into context burns tokens with no value. If you need bytes, use exec with `strings`, `xxd`, or `file` deliberately.]", abs, size), true
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

const readDescription = `Read one file.
  - skeleton: signatures only. ~10x cheaper than full.
  - slice: lines [offset, offset+limit).
  - full: entire file.`

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
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
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
			if msg, binary := binaryFileRefusal(abs); binary {
				return msg, nil
			}
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

	// Use bufio.Reader.ReadString('\n') instead of bufio.Scanner: Scanner
	// has a fixed token cap (default 64 KiB, we previously raised it to
	// 1 MiB) and aborts with "token too long" on any single line beyond it.
	// minified bundles, generated SQL, and certain logs routinely exceed
	// that. ReadString grows naturally; we still cap the per-line bytes we
	// emit so a 50 MiB line doesn't blow the response.
	const lineDisplayCap = 8192
	r := bufio.NewReader(f)
	var out []string
	line := 0
	for {
		line++
		text, err := r.ReadString('\n')
		eof := err != nil
		if err != nil && err != io.EOF {
			return "", err
		}
		if line >= offset {
			text = strings.TrimRight(text, "\n")
			if len(text) > lineDisplayCap {
				text = text[:lineDisplayCap] + "...[line truncated]"
			}
			out = append(out, fmt.Sprintf("%d\t%s", line, text))
			if len(out) >= limit {
				break
			}
		}
		if eof {
			break
		}
	}
	if len(out) == 0 {
		return "[empty range]", nil
	}
	return strings.Join(out, "\n"), nil
}

func readFullMode(abs string) (string, error) {
	fi, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	max := int64(readMaxBytes())
	if fi.Size() > max {
		return fmt.Sprintf("[full read refused: %s is %d bytes (>%d cap). Use mode=slice to page through, or raise AXON_READ_MAX_BYTES.]", abs, fi.Size(), max), nil
	}
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

	// Same reasoning as readSliceMode: Scanner trips on long lines.
	const skelLineCap = 4096
	r := bufio.NewReader(f)
	var out []string
	line := 0
	for {
		line++
		text, err := r.ReadString('\n')
		eof := err != nil
		if err != nil && err != io.EOF {
			return "", err
		}
		text = strings.TrimRight(text, "\n")
		if len(text) <= skelLineCap {
			for _, re := range skeletonPatterns {
				if re.MatchString(text) {
					out = append(out, fmt.Sprintf("%d\t%s", line, text))
					break
				}
			}
		}
		if eof {
			break
		}
	}
	if len(out) == 0 {
		return "[skeleton found no signatures]", nil
	}
	return strings.Join(out, "\n"), nil
}

// ---------------------------------------------------------------------------
// WRITE — five modes. Each mode has a deterministic contract.
// ---------------------------------------------------------------------------

const writeDescription = `Write to a file.
  - save: set the file's full contents. Creates if absent, replaces if present. Use this whenever you have the whole file in hand — do not check existence first.
  - replace_string: replace one exact occurrence of old_str.
  - replace_lines: replace lines [start_line, end_line].
  - insert_at_line: insert before start_line (1-based).

Writes are atomic (tmp + rename) and reversible via /undo. A formatter runs after every write. For brace languages emit content flat (no indentation). For whitespace-significant languages (Python, YAML) emit indentation correctly.`

func WriteTool(s *Session) Tool {
	return Tool{
		Name:        toolWrite,
		Description: writeDescription,
		Schema: obj("object", props{
			"path":       strSchema("Relative or absolute file path."),
			"mode":       enumSchema("save | replace_string | replace_lines | insert_at_line. Required.", writeSave, writeReplaceStr, writeReplaceLn, writeInsertAt),
			"content":    strSchema("New content. Required for all modes."),
			"old_str":    strSchema("Exact text to replace. Required when mode=replace_string."),
			"start_line": intSchema("1-based start line. Required when mode=replace_lines or insert_at_line."),
			"end_line":   intSchema("1-based end line, inclusive. Required when mode=replace_lines."),
			"reason":     reasonField(),
		}, []string{"path", "mode", "content", "reason"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
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
			case writeSave:
				return writeSaveMode(s, abs, p.Content)
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
				return "", fmt.Errorf("mode is required: save | replace_string | replace_lines | insert_at_line")
			}
		},
	}
}

// writeSaveMode sets the file's full contents. Creates the file (and any
// missing parent dirs) if absent, replaces it if present. Reports which
// happened in the result string for the agent's benefit, but never errors on
// existence — the agent is declaring intent, not checking state.
func writeSaveMode(s *Session, abs, content string) (string, error) {
	before, statErr := os.ReadFile(abs)
	existed := statErr == nil
	if dir := filepath.Dir(abs); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}
	priorContent := ""
	if existed {
		priorContent = string(before)
	}
	s.RecordEdit(abs, priorContent, content)
	if err := writeBytes(abs, []byte(content)); err != nil {
		return "", err
	}
	if existed {
		return "saved " + abs + " (replaced)", nil
	}
	return "saved " + abs + " (created)", nil
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

const searchDescription = `Search across files.
  - literal: exact string.
  - regex: regex pattern.
  - trace: symbol — returns definition + callers + callees.`

func SearchTool(s *Session) Tool {
	return Tool{
		Name:        toolSearch,
		Description: searchDescription,
		Schema: obj("object", props{
			"query":          strSchema("Text, regex pattern, or symbol name (depending on mode)."),
			"mode":           enumSchema("literal | regex | trace. Required.", searchLiteral, searchRegex, searchTrace),
			"path":           strSchema("Optional search root. Default '.'."),
			"globs":          arr(strSchema("Optional rg glob filters, e.g. '*.go'.")),
			"case_sensitive": boolSchema("Match case. Default false (rg --ignore-case)."),
			"max_matches":    intSchema("Cap total matches. Defaults to AXON_SEARCH_LIMIT."),
			"reason":         reasonField(),
		}, []string{"query", "mode", "reason"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				Query         string   `json:"query"`
				Mode          string   `json:"mode"`
				Path          string   `json:"path"`
				Globs         []string `json:"globs"`
				CaseSensitive bool     `json:"case_sensitive"`
				MaxMatches    int      `json:"max_matches"`
				Reason        string   `json:"reason"`
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
			if p.MaxMatches <= 0 {
				p.MaxMatches = searchLimit()
			}
			switch p.Mode {
			case searchLiteral:
				return runRipgrep(ctx, s, p.Query, p.Path, p.Globs, true, p.CaseSensitive, p.MaxMatches)
			case searchRegex:
				return runRipgrep(ctx, s, p.Query, p.Path, p.Globs, false, p.CaseSensitive, p.MaxMatches)
			case searchTrace:
				return runTrace(ctx, s, p.Query, p.Path, p.Globs, p.MaxMatches)
			default:
				return "", fmt.Errorf("mode is required: literal | regex | trace")
			}
		},
	}
}

func runRipgrep(parent context.Context, s *Session, query, path string, globs []string, literal, caseSensitive bool, maxMatches int) (string, error) {
	args := []string{"-n", "--no-heading", "--color", "never", "-g", "!.git", "--hidden"}
	if !caseSensitive {
		args = append(args, "--ignore-case")
	}
	if literal {
		args = append(args, "--fixed-strings")
	}
	if maxMatches > 0 {
		args = append(args, "--max-count", fmt.Sprintf("%d", maxMatches))
	}
	for _, g := range globs {
		if g = strings.TrimSpace(g); g != "" {
			args = append(args, "-g", g)
		}
	}
	args = append(args, "--", query, path)

	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(searchTimeoutSeconds())*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "rg", args...)
	if s.Cwd != "" {
		cmd.Dir = s.Cwd
	}
	buf := &limitBuf{limit: searchOutputLimit()}
	cmd.Stdout = buf
	cmd.Stderr = buf
	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("search timed out after %ds", searchTimeoutSeconds())
		}
		if parent.Err() != nil {
			return "", parent.Err()
		}
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
		b.WriteString("\n[output truncated]")
	}
	return b.String(), nil
}

// runTrace: regex-based symbol trace. Finds definitions, callers, and callees.
// Heuristic. Good enough for ~80% of cases. Tree-sitter upgrade is future work.
func runTrace(parent context.Context, s *Session, symbol, path string, globs []string, maxMatches int) (string, error) {
	// Two def patterns OR'd together:
	//   1. plain decl:   func Foo, function Foo, def Foo, class Foo, ...
	//   2. Go method:    func (recv T) Foo
	// Without (2) the trace shows "<not found>" for any method, which is
	// the most common Go callable.
	q := regexp.QuoteMeta(symbol)
	defPattern := fmt.Sprintf(`(func|function|def|class|type|struct|interface)\s+%s\b|func\s+\([^)]*\)\s+%s\b`, q, q)
	callPattern := fmt.Sprintf(`\b%s\s*\(`, q)

	defs, err := rgCollect(parent, s, defPattern, path, globs, maxMatches)
	if err != nil {
		return "", err
	}
	calls, err := rgCollect(parent, s, callPattern, path, globs, maxMatches)
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
	b.WriteString("\n[trace: regex heuristic. May miss method calls on receivers or shadowed names.]")
	return b.String(), nil
}

type rgHit struct {
	file string
	line int
	text string
}

func rgCollect(parent context.Context, s *Session, pattern, path string, globs []string, maxMatches int) ([]rgHit, error) {
	// Use --field-context-separator and a NUL byte separator pair? Simpler:
	// emit JSON with --json and parse, but that's a bigger change. Instead
	// use a fixed-width separator that real paths can't contain. rg already
	// guarantees this with --field-match-separator, but availability varies;
	// fall back to colon parsing that's robust to colons in paths by
	// matching `:<digits>:` at the end of the prefix.
	args := []string{"-n", "--no-heading", "--color", "never", "-g", "!.git"}
	if maxMatches > 0 {
		args = append(args, "--max-count", fmt.Sprintf("%d", maxMatches))
	}
	for _, g := range globs {
		if g = strings.TrimSpace(g); g != "" {
			args = append(args, "-g", g)
		}
	}
	args = append(args, "--", pattern, path)
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(searchTimeoutSeconds())*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "rg", args...)
	if s.Cwd != "" {
		cmd.Dir = s.Cwd
	}
	buf := &limitBuf{limit: searchOutputLimit()}
	cmd.Stdout = buf
	cmd.Stderr = buf
	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("search timed out after %ds", searchTimeoutSeconds())
		}
		if parent.Err() != nil {
			return nil, parent.Err()
		}
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
		// rg format: file:line:text — but file paths may contain ':'.
		// Parse from the right: find the last ":<digits>:" anchor.
		file, lineNum, text, ok := parseRgLine(ln)
		if !ok {
			continue
		}
		hits = append(hits, rgHit{file: file, line: lineNum, text: text})
	}
	return hits, nil
}

// parseRgLine splits an rg "file:line:text" record. File paths may legally
// contain ':' (e.g. on URLs accidentally indexed, or rare filenames), so we
// scan from the left for the first ":<digits>:" boundary. A line number
// always immediately follows the path on a single record line.
func parseRgLine(ln string) (string, int, string, bool) {
	// Walk forward looking for ":N:" where N is one-or-more digits.
	for i := 0; i < len(ln); i++ {
		if ln[i] != ':' {
			continue
		}
		j := i + 1
		for j < len(ln) && ln[j] >= '0' && ln[j] <= '9' {
			j++
		}
		if j == i+1 || j >= len(ln) || ln[j] != ':' {
			continue
		}
		var n int
		fmt.Sscanf(ln[i+1:j], "%d", &n)
		return ln[:i], n, ln[j+1:], true
	}
	return "", 0, "", false
}

// ---------------------------------------------------------------------------
// EXEC — non-interactive shell command. LLM controls tail size.
// ---------------------------------------------------------------------------

const execDescription = `Run a shell command.
  - run: arbitrary non-interactive command. tail_lines required.
  - verify: auto-detected build/type-check (go build, tsc, cargo check, …).
Set run_in_background=true for any command that *might* wait — servers, watchers, HTTP clients (curl/wget against any service, including ones you just started), database clients, anything reading stdin or a socket, anything connecting to a host you don't fully control. The rule is the chance of hanging, not the certainty: if you'd be surprised by either outcome, go background. Foreground is for commands you know terminate on their own (build, vet, test, format, file I/O, deterministic CPU work). Background returns a shell_id immediately; use bash_output to read logs and kill_shell to stop.
Stdin is always /dev/null — interactive commands (prompts, REPLs, password reads) WILL hang. Use non-interactive flags (-y, --yes, --non-interactive) instead.`

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
		// find -exec ... + handles paths with spaces; -print0/xargs -0 isn't
		// portable to BSD find without -print0 support, so use -exec which
		// works the same on GNU and BSD. python -m compileall walks the tree
		// itself, exiting 0 when every file compiles.
		{"pyproject.toml", "python -m compileall -q ."},
		{"setup.py", "python -m compileall -q ."},
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
			"mode":              enumSchema("run | verify. Required.", execRun, execVerify),
			"command":           strSchema("Shell command. Required for mode=run."),
			"tail_lines":        intSchema("Last N lines to keep. Required for mode=run; defaults to 50 for mode=verify. Ignored when run_in_background=true."),
			"expected_outcome":  strSchema("What success looks like. Optional but enables structured failure diagnosis."),
			"dir":               strSchema("Optional working directory override."),
			"timeout_seconds":   intSchema(fmt.Sprintf("Default %d. Ignored when run_in_background=true.", timeout)),
			"run_in_background": boolSchema("Spawn detached and return a shell_id immediately. Use for servers, watchers, anything long-running. Default false."),
			"reason":            reasonField(),
		}, []string{"mode", "reason"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				Mode            string `json:"mode"`
				Command         string `json:"command"`
				TailLines       int    `json:"tail_lines"`
				ExpectedOutcome string `json:"expected_outcome"`
				Dir             string `json:"dir"`
				TimeoutSeconds  int    `json:"timeout_seconds"`
				RunInBackground bool   `json:"run_in_background"`
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
					p.TailLines = execDefaultTailLines()
				}
				if p.ExpectedOutcome == "" {
					p.ExpectedOutcome = "no errors"
				}
			case execRun:
				if strings.TrimSpace(p.Command) == "" {
					return "", fmt.Errorf("command is required for mode=run")
				}
				if !p.RunInBackground && p.TailLines <= 0 {
					return "", fmt.Errorf("tail_lines is required and must be > 0 for mode=run")
				}
			default:
				return "", fmt.Errorf("mode is required: run | verify")
			}
			// Cap tail_lines so the LLM cannot request a huge tail that
			// blows the context regardless of execOutputLimit byte cap.
			if max := execMaxTailLines(); p.TailLines > max {
				p.TailLines = max
			}

			if p.RunInBackground {
				if p.Mode == execVerify {
					return "", fmt.Errorf("run_in_background is not valid with mode=verify")
				}
				sh, err := bgReg.start(p.Command, resolvedDir)
				if err != nil {
					return "", err
				}
				return formatBgStart(sh), nil
			}

			if p.TimeoutSeconds <= 0 {
				p.TimeoutSeconds = timeout
			}
			// Cap user-supplied timeout so a runaway tool call cannot hold
			// the turn forever.
			if max := execMaxTimeoutSeconds(); p.TimeoutSeconds > max {
				p.TimeoutSeconds = max
			}

			// Derive from the turn ctx so Ctrl-C cancels the running command.
			parent := ctx
			if parent == nil {
				parent = context.Background()
			}
			runCtx, cancel := context.WithTimeout(parent, time.Duration(p.TimeoutSeconds)*time.Second)
			defer cancel()

			cmd := exec.Command("sh", "-lc", p.Command)
			// Put the shell and all its descendants in their own process group
			// so we can kill the whole tree on timeout. Without this, the shell
			// dies but grandchildren (curl, server connections) survive holding
			// the stdout/stderr pipes open — cmd.Wait() then blocks forever even
			// after the context fires. Same trick bg.go uses for backgrounded
			// shells, applied here so foreground exec actually honors timeouts.
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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

			if err := cmd.Start(); err != nil {
				return "", err
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			var runErr error
			select {
			case runErr = <-done:
			case <-runCtx.Done():
				// Kill the whole process group, not just the shell. The negative
				// PID is the syscall convention for "this process group."
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				runErr = <-done
			}
			code := 0
			note := ""
			if runErr != nil {
				switch {
				case runCtx.Err() == context.DeadlineExceeded:
					code = -1
					note = "timed out"
				case parent.Err() != nil:
					// Parent (turn) ctx cancelled — Ctrl-C or shutdown.
					code = -1
					note = "cancelled"
				default:
					if exitErr, ok := runErr.(*exec.ExitError); ok {
						code = exitErr.ExitCode()
					} else {
						return "", runErr
					}
				}
			}

			tailed, hidden := tailN(buf.buf.String(), p.TailLines)
			return formatExec(p.Command, cmd.Dir, code, p.ExpectedOutcome, tailed, hidden, buf.truncated, note), nil
		},
	}
}

const bashOutputDescription = `Read new output from a background shell since the last read. Status is "running" or the exit summary. Returns only the delta — calling this in a poll loop is cheap; rereading the same bytes is not.
  - tail_lines: optional. Keep only the last N lines of the delta. Useful for chatty servers.
  - max_bytes: optional. Cap returned bytes (tail kept). Default ~32 KiB; offset still advances past dropped bytes so the next call continues from "now."`

func BashOutputTool(s *Session) Tool {
	return Tool{
		Name:        toolBashOutput,
		Description: bashOutputDescription,
		Schema: obj("object", props{
			"shell_id":   strSchema("Background shell handle, e.g. bash_1."),
			"tail_lines": intSchema("Optional. Keep only the last N lines of the new delta."),
			"max_bytes":  intSchema("Optional. Cap returned bytes (tail kept). Default ~32 KiB."),
			"reason":     reasonField(),
		}, []string{"shell_id", "reason"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				ShellID   string `json:"shell_id"`
				TailLines int    `json:"tail_lines"`
				MaxBytes  int    `json:"max_bytes"`
				Reason    string `json:"reason"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}
			sh, ok := bgReg.get(p.ShellID)
			if !ok {
				return "", fmt.Errorf("unknown shell_id: %s", p.ShellID)
			}
			cap := p.MaxBytes
			if cap <= 0 {
				cap = bashOutputMaxBytes()
			}
			out, byteTrunc, err := sh.readNew(cap)
			if err != nil {
				return "", err
			}
			lineTrunc := 0
			if p.TailLines > 0 && out != "" {
				out, lineTrunc = tailN(out, p.TailLines)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "shell_id: %s\nstatus: %s\n", sh.ID, sh.status())
			if byteTrunc {
				b.WriteString("[earlier delta bytes dropped at max_bytes — log offset still advanced]\n")
			}
			if lineTrunc > 0 {
				fmt.Fprintf(&b, "[%d earlier delta lines dropped at tail_lines]\n", lineTrunc)
			}
			if out == "" {
				b.WriteString("(no new output)\n")
			} else {
				b.WriteString("---\n")
				b.WriteString(out)
				if !strings.HasSuffix(out, "\n") {
					b.WriteString("\n")
				}
			}
			return b.String(), nil
		},
	}
}

const killShellDescription = `Stop a background shell (SIGTERM, then SIGKILL after grace). Always kill servers you started — sessions do not leak processes, but cleaning up early frees ports.`

func KillShellTool(s *Session) Tool {
	return Tool{
		Name:        toolKillShell,
		Description: killShellDescription,
		Schema: obj("object", props{
			"shell_id": strSchema("Background shell handle, e.g. bash_1."),
			"reason":   reasonField(),
		}, []string{"shell_id", "reason"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				ShellID string `json:"shell_id"`
				Reason  string `json:"reason"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}
			sh, ok := bgReg.get(p.ShellID)
			if !ok {
				return "", fmt.Errorf("unknown shell_id: %s", p.ShellID)
			}
			if err := sh.kill(2 * time.Second); err != nil {
				return "", err
			}
			return fmt.Sprintf("shell_id: %s\nstatus: %s\n", sh.ID, sh.status()), nil
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
