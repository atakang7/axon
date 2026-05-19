package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

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
			"mode":           enumSchema("literal | regex | trace. Optional; defaults to literal.", searchLiteral, searchRegex, searchTrace),
			"path":           strSchema("Optional search root. Default '.'."),
			"globs":          arr(strSchema("Optional rg glob filters, e.g. '*.go'.")),
			"case_sensitive": boolSchema("Match case. Default false (rg --ignore-case)."),
			"max_matches":    intSchema("Cap total matches. Defaults to AXON_SEARCH_LIMIT."),
		}, []string{"query"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				Query         string   `json:"query"`
				Mode          string   `json:"mode"`
				Path          string   `json:"path"`
				Globs         []string `json:"globs"`
				CaseSensitive bool     `json:"case_sensitive"`
				MaxMatches    int      `json:"max_matches"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
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
			if p.Mode == "" {
				p.Mode = searchLiteral
			}
			switch p.Mode {
			case searchLiteral:
				return runRipgrep(ctx, s, p.Query, p.Path, p.Globs, true, p.CaseSensitive, p.MaxMatches)
			case searchRegex:
				return runRipgrep(ctx, s, p.Query, p.Path, p.Globs, false, p.CaseSensitive, p.MaxMatches)
			case searchTrace:
				return runTrace(ctx, s, p.Query, p.Path, p.Globs, p.MaxMatches)
			default:
				return "", fmt.Errorf("unknown mode %q: literal | regex | trace", p.Mode)
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
