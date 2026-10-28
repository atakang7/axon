package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// The tool set every coding model already knows: read_file, edit,
// write_file, bash, grep, glob, todo_write. Models are trained on this shape;
// an interface of our own (write modes, read modes, a goal tool, a checkpoint
// tool) is one more thing a small model gets wrong. See HARNESS.md and
// BEHAVIORS.md.
//
// The behaviour lives inside the tools, where no model can skip it:
//
//   - A file is edited only after it was read, and only while it is as it was
//     read. An edit is then an edit of text the model has seen, never of a
//     file it imagines.
//   - Every edit reports whether what it wrote still parses and builds.
//   - The shell keeps its directory and exports, like a terminal.

// fileLedger remembers which files the model has seen, as they were then.
type fileLedger struct {
	mu   sync.Mutex
	seen map[string]fileStamp
}

type fileStamp struct {
	size int64
	mod  time.Time
}

func stampOf(path string) (fileStamp, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fileStamp{}, false
	}
	return fileStamp{size: info.Size(), mod: info.ModTime()}, true
}

// saw records path as the model now knows it.
func (l *fileLedger) saw(path string) {
	if l == nil {
		return
	}
	st, ok := stampOf(path)
	if !ok {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen == nil {
		l.seen = map[string]fileStamp{}
	}
	l.seen[filepath.Clean(path)] = st
}

// mayChange reports why the model may not change path yet, or "" when it
// may. A file that does not exist may always be created.
func (l *fileLedger) mayChange(path string) string {
	if l == nil {
		return ""
	}
	now, exists := stampOf(path)
	if !exists {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	before, ok := l.seen[filepath.Clean(path)]
	if !ok {
		return fmt.Sprintf("%s has not been read in this session. Read it with read_file first: an edit must be based on the file as it is.", path)
	}
	if before != now {
		return fmt.Sprintf("%s changed since you last read it. Read it again with read_file, then edit what is there now.", path)
	}
	return ""
}

// ledgerOf is the session's ledger; workspaces without one (tests) apply no
// read-before-edit rule.
func ledgerOf(ws Workspace) *fileLedger {
	if s, ok := ws.(*Session); ok {
		s.ledgerOnce.Do(func() { s.files = &fileLedger{} })
		return s.files
	}
	return nil
}

// ---------------------------------------------------------------------------
// read_file
// ---------------------------------------------------------------------------

const readFileDescription = `Read a file, with line numbers ("N<tab>text"). Reads up to 2000 lines from offset (1-based); pass offset and limit for a part of a long file. A directory returns its tree two levels deep.
Read every file you will edit before editing it. Read several files at once by making several read_file calls in one reply.`

// ReadFileTool reads a file or lists a directory.
func ReadFileTool(ws Workspace, lim Limits) Tool {
	return Tool{
		Name:        toolRead,
		Description: readFileDescription,
		Schema: obj("object", props{
			"path":   strSchema("File or directory, absolute or relative to the working directory."),
			"offset": intSchema("1-based line to start at. Optional."),
			"limit":  intSchema("Number of lines. Optional; default 2000."),
		}, []string{"path"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				Path   string `json:"path"`
				Offset int    `json:"offset"`
				Limit  int    `json:"limit"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Path) == "" {
				return "", fmt.Errorf("path is required")
			}
			abs := ws.ResolvePath(p.Path)
			info, err := os.Stat(abs)
			if err != nil {
				return "", err
			}
			if info.IsDir() {
				return dirTree(abs)
			}
			if msg, binary := binaryFileRefusal(abs); binary {
				return msg, nil
			}
			if p.Offset < 1 {
				p.Offset = 1
			}
			if p.Limit <= 0 {
				p.Limit = 2000
			}
			out, err := readSliceMode(abs, p.Offset, p.Limit)
			if err != nil {
				return "", err
			}
			ledgerOf(ws).saw(abs)
			if total := countLines(abs); total > p.Offset+p.Limit-1 {
				out += fmt.Sprintf("\n[lines %d-%d of %d; read on with offset %d]", p.Offset, p.Offset+p.Limit-1, total, p.Offset+p.Limit)
			}
			return out, nil
		},
	}
}

func countLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return lineCount(strings.TrimSuffix(string(data), "\n"))
}

// ---------------------------------------------------------------------------
// edit and write_file
// ---------------------------------------------------------------------------

const editDescription = `Replace exact text in a file. old_string must appear in the file exactly once (copy it from read_file output, without the line-number prefix, with enough surrounding lines to be unique), or set replace_all to replace every occurrence. The file must have been read first.
For several changes, make several edit calls in one reply. The result ends with a check of what was written (Go is formatted and built; JSON, Python and JavaScript are syntax-checked): "check: FAILED" means the edit broke something; fix that first.`

// EditTool replaces exact text in one file.
func EditTool(ws Workspace) Tool {
	return Tool{
		Name:        toolEdit,
		Description: editDescription,
		Schema: obj("object", props{
			"path":        strSchema("File to change."),
			"old_string":  strSchema("Exact text to replace."),
			"new_string":  strSchema("Replacement text."),
			"replace_all": boolSchema("Replace every occurrence. Default false."),
		}, []string{"path", "old_string", "new_string"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				Path       string `json:"path"`
				OldString  string `json:"old_string"`
				NewString  string `json:"new_string"`
				ReplaceAll bool   `json:"replace_all"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Path) == "" {
				return "", fmt.Errorf("path is required")
			}
			abs := ws.ResolvePath(p.Path)
			if _, err := os.Stat(abs); err != nil {
				return "", fmt.Errorf("%s does not exist; create it with write_file", p.Path)
			}
			ledger := ledgerOf(ws)
			if why := ledger.mayChange(abs); why != "" {
				return "", fmt.Errorf("%s", why)
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				return "", err
			}
			raw0 := string(data)
			text, crlf := splitEOL(raw0)
			oldS, _ := splitEOL(p.OldString)
			newS, _ := splitEOL(p.NewString)
			switch n := strings.Count(text, oldS); {
			case oldS == "":
				return "", fmt.Errorf("old_string is empty; to create or replace a whole file use write_file")
			case oldS == newS:
				return "", fmt.Errorf("old_string and new_string are identical: nothing would change")
			case n == 0:
				return "", fmt.Errorf("old_string was not found in %s. Copy it exactly from read_file output (without the line-number prefix), including whitespace", p.Path)
			case n > 1 && !p.ReplaceAll:
				return "", fmt.Errorf("old_string appears %d times in %s (%s). Add surrounding lines to make it unique, or set replace_all", n, p.Path, matchLineSummary(text, oldS))
			default:
				count := 1
				if p.ReplaceAll {
					count = n
					text = strings.ReplaceAll(text, oldS, newS)
				} else {
					text = strings.Replace(text, oldS, newS, 1)
				}
				ws.RecordEdit(abs, raw0)
				if err := WriteFileAtomic(abs, []byte(joinEOL(text, crlf))); err != nil {
					return "", err
				}
				out := fmt.Sprintf("edited %s (%d replaced)", p.Path, count)
				if check := afterWrite(ctx, []string{abs}); check != "" {
					out += "\n" + check
				}
				ledger.saw(abs)
				return out, nil
			}
		},
	}
}

const writeFileDescription = `Write a whole file: create a new one, or replace an existing one completely. An existing file must have been read first. To change part of a file, use edit instead. The result ends with a check of what was written.`

// WriteFileTool creates or replaces a whole file.
func WriteFileTool(ws Workspace) Tool {
	return Tool{
		Name:        toolWrite,
		Description: writeFileDescription,
		Schema: obj("object", props{
			"path":    strSchema("File to write."),
			"content": strSchema("The file's full contents."),
		}, []string{"path", "content"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				Path    string  `json:"path"`
				Content *string `json:"content"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Path) == "" || p.Content == nil {
				return "", fmt.Errorf("path and content are required")
			}
			abs := ws.ResolvePath(p.Path)
			ledger := ledgerOf(ws)
			if why := ledger.mayChange(abs); why != "" {
				return "", fmt.Errorf("%s", why)
			}
			out, err := writeSaveMode(ws, abs, *p.Content)
			if err != nil {
				return "", err
			}
			if check := afterWrite(ctx, []string{abs}); check != "" {
				out += "\n" + check
			}
			ledger.saw(abs)
			return out, nil
		},
	}
}

// ---------------------------------------------------------------------------
// bash
// ---------------------------------------------------------------------------

const bashDescription = `Run a shell command and return its output and exit code. The shell keeps its working directory and exported variables between calls, like a terminal. Long output keeps its last lines plus any error lines from the part cut off.
Use run_in_background for servers, watchers and anything that may not finish on its own; it returns a shell_id for bash_output and kill_shell. Commands get no stdin: pass flags such as -y instead of waiting for a prompt. Prefer read_file, grep and glob over cat, grep and find.`

// BashTool runs shell commands.
func BashTool(ws Workspace, shells *BackgroundShells, lim Limits) Tool {
	exec := ExecTool(ws, shells, lim)
	return Tool{
		Name:        toolExec,
		Description: bashDescription,
		Schema: obj("object", props{
			"command":           strSchema("The command."),
			"timeout":           intSchema(fmt.Sprintf("Seconds before it is stopped. Default %d, at most %d.", int(lim.ExecTimeout.Seconds()), int(lim.ExecMaxTimeout.Seconds()))),
			"run_in_background": boolSchema("Start it and return at once with a shell_id. Default false."),
		}, []string{"command"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				Command         string `json:"command"`
				Timeout         int    `json:"timeout"`
				RunInBackground bool   `json:"run_in_background"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			inner, _ := json.Marshal(map[string]any{"command": p.Command, "timeout_seconds": p.Timeout, "run_in_background": p.RunInBackground})
			return exec.Fn(ctx, inner)
		},
	}
}

// ---------------------------------------------------------------------------
// grep and glob
// ---------------------------------------------------------------------------

const grepDescription = `Search file contents with a regular expression (ripgrep syntax). Returns matching lines as path:line:text. Narrow with path (a directory or file) and glob (e.g. "*.go"). Use it to find definitions, callers and uses before reading.`

// GrepTool searches file contents.
func GrepTool(ws Workspace, lim Limits) Tool {
	search := SearchTool(ws, lim)
	return Tool{
		Name:        toolSearch,
		Description: grepDescription,
		Schema: obj("object", props{
			"pattern":          strSchema("Regular expression."),
			"path":             strSchema("Directory or file to search. Default: the working directory."),
			"glob":             strSchema("Only files matching this glob, e.g. \"*.go\" or \"src/**/*.ts\"."),
			"case_insensitive": boolSchema("Ignore case. Default false."),
		}, []string{"pattern"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				Pattern         string `json:"pattern"`
				Path            string `json:"path"`
				Glob            string `json:"glob"`
				CaseInsensitive bool   `json:"case_insensitive"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			in := map[string]any{"query": p.Pattern, "mode": searchRegex, "path": p.Path, "case_sensitive": !p.CaseInsensitive}
			if p.Glob != "" {
				in["globs"] = []string{p.Glob}
			}
			inner, _ := json.Marshal(in)
			return search.Fn(ctx, inner)
		},
	}
}

const globDescription = `Find files by name pattern, e.g. "**/*.go" or "src/**/handler*.ts". Returns matching paths, relative to the search root, generated and tool directories left out.`

// maxGlobResults bounds one glob.
const maxGlobResults = 300

// GlobTool finds files by name.
func GlobTool(ws Workspace) Tool {
	return Tool{
		Name:        toolGlob,
		Description: globDescription,
		Schema: obj("object", props{
			"pattern": strSchema("Glob; ** matches any number of directories."),
			"path":    strSchema("Directory to search. Default: the working directory."),
		}, []string{"pattern"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				Pattern string `json:"pattern"`
				Path    string `json:"path"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			root := ws.Dir()
			if strings.TrimSpace(p.Path) != "" {
				root = ws.ResolvePath(p.Path)
			}
			re, err := globRegexp(p.Pattern)
			if err != nil {
				return "", err
			}
			var out []string
			more := 0
			_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					if path != root && (skipDirs[d.Name()] || d.Name() == ".git") {
						return filepath.SkipDir
					}
					return nil
				}
				rel, _ := filepath.Rel(root, path)
				rel = filepath.ToSlash(rel)
				if re.MatchString(rel) {
					if len(out) < maxGlobResults {
						out = append(out, rel)
					} else {
						more++
					}
				}
				return nil
			})
			if len(out) == 0 {
				return "[no files match " + p.Pattern + "]", nil
			}
			sort.Strings(out)
			res := strings.Join(out, "\n")
			if more > 0 {
				res += fmt.Sprintf("\n[%d more; narrow the pattern]", more)
			}
			return res, nil
		},
	}
}

// globRegexp compiles a glob with ** into a regular expression over
// slash-separated relative paths. A pattern without a slash matches the base
// name anywhere, as most tools do.
func globRegexp(pattern string) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	if !strings.Contains(pattern, "/") {
		pattern = "**/" + pattern
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '{':
			b.WriteString("(?:")
		case c == '}':
			b.WriteString(")")
		case c == ',':
			b.WriteString("|")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// ---------------------------------------------------------------------------
// todo_write
// ---------------------------------------------------------------------------

const todoDescription = `Keep the plan for multi-step work: the whole list each time, each item pending, in_progress or completed, exactly one in_progress while working. Write it before the first change of a multi-step task and update it as items finish. Skip it for single-step work.`

// Todo is one item of the plan.
type Todo struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

// TodoTool keeps the session's plan.
func TodoTool(s *Session) Tool {
	return Tool{
		Name:        toolTask,
		Description: todoDescription,
		Schema: obj("object", props{
			"todos": arr(obj("object", props{
				"content": strSchema("The step, as a short imperative."),
				"status":  enumSchema("pending | in_progress | completed.", "pending", "in_progress", "completed"),
			}, []string{"content", "status"})),
		}, []string{"todos"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				Todos []Todo `json:"todos"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			active := 0
			for i, t := range p.Todos {
				switch t.Status {
				case "pending", "completed":
				case "in_progress":
					active++
				default:
					return "", fmt.Errorf("todo %d: status must be pending, in_progress or completed", i+1)
				}
			}
			if active > 1 {
				return "", fmt.Errorf("%d items are in_progress; keep exactly one", active)
			}
			s.Todos = p.Todos
			return "plan updated\n" + s.todoLines(), s.Save()
		},
	}
}

func (s *Session) todoLines() string {
	var b strings.Builder
	for _, t := range s.Todos {
		mark := "[ ]"
		switch t.Status {
		case "completed":
			mark = "[x]"
		case "in_progress":
			mark = "[>]"
		}
		b.WriteString(mark + " " + t.Content + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
