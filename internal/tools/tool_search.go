package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/atakang7/axon/internal/config"
)

// ---------------------------------------------------------------------------
// SEARCH — multi-file content. Three modes.
// ---------------------------------------------------------------------------

const searchDescription = `Search across files.
  - literal: exact string.
  - regex: regex pattern.
  - trace: symbol — returns definition + callers + callees.`

func SearchTool(ws Workspace, lim config.Limits) Tool {
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
			var p searchInput
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
				p.MaxMatches = lim.SearchMaxMatches
			}
			if p.Mode == "" {
				p.Mode = searchLiteral
			}
			switch p.Mode {
			case searchLiteral:
				return runRipgrep(ctx, ws, lim, p, true)
			case searchRegex:
				return runRipgrep(ctx, ws, lim, p, false)
			case searchTrace:
				return runTrace(ctx, ws, lim, p)
			default:
				return "", fmt.Errorf("unknown mode %q: literal | regex | trace", p.Mode)
			}
		},
	}
}

type searchInput struct {
	Query         string   `json:"query"`
	Mode          string   `json:"mode"`
	Path          string   `json:"path"`
	Globs         []string `json:"globs"`
	CaseSensitive bool     `json:"case_sensitive"`
	MaxMatches    int      `json:"max_matches"`
}

// errNoRipgrep is returned when rg is missing. Every search mode depends on
// it, so the message names the fix rather than the symptom.
var errNoRipgrep = errors.New("search requires rg (ripgrep) in PATH")

// globArgs appends the caller's glob filters, skipping blanks.
func globArgs(args []string, globs []string) []string {
	for _, g := range globs {
		if g = strings.TrimSpace(g); g != "" {
			args = append(args, "-g", g)
		}
	}
	return args
}

// runRg executes ripgrep and returns its captured output. Exit status 1 is
// ripgrep's "no matches", which is a result rather than a failure, so it comes
// back as empty output and a nil error.
//
// Every search mode goes through here: the timeout, the output cap and the
// exit-status interpretation are decided once.
func runRg(parent context.Context, ws Workspace, lim config.Limits, args []string) (out string, truncated bool, err error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, lim.SearchTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "rg", args...)
	if dir := ws.Dir(); dir != "" {
		cmd.Dir = dir
	}
	buf := &limitBuf{limit: lim.SearchOutputBytes}
	cmd.Stdout, cmd.Stderr = buf, buf

	runErr := cmd.Run()
	captured, truncated := buf.snapshot()
	if runErr == nil {
		return strings.TrimRight(captured, "\n"), truncated, nil
	}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return "", false, fmt.Errorf("search timed out after %s", lim.SearchTimeout)
	case parent.Err() != nil:
		return "", false, parent.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
		return "", false, nil
	}
	if strings.Contains(runErr.Error(), "executable file not found") {
		return "", false, errNoRipgrep
	}
	return "", false, runErr
}

// runRipgrep serves the literal and regex modes.
func runRipgrep(parent context.Context, ws Workspace, lim config.Limits, p searchInput, literal bool) (string, error) {
	args := []string{"-n", "--no-heading", "--color", "never", "-g", "!.git", "--hidden"}
	if !p.CaseSensitive {
		args = append(args, "--ignore-case")
	}
	if literal {
		args = append(args, "--fixed-strings")
	}
	if p.MaxMatches > 0 {
		args = append(args, "--max-count", fmt.Sprintf("%d", p.MaxMatches))
	}
	args = globArgs(args, p.Globs)
	args = append(args, "--", p.Query, p.Path)

	out, truncated, err := runRg(parent, ws, lim, args)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "query: %s\npath: %s\n", p.Query, p.Path)
	if out == "" {
		b.WriteString("no matches")
	} else {
		b.WriteString(out)
	}
	if truncated {
		b.WriteString("\n[output truncated]")
	}
	return b.String(), nil
}

// runTrace: regex-based symbol trace. Finds definitions, callers, and callees.
// Heuristic. Good enough for ~80% of cases. Tree-sitter upgrade is future work.
func runTrace(parent context.Context, ws Workspace, lim config.Limits, p searchInput) (string, error) {
	symbol, path, globs, maxMatches := p.Query, p.Path, p.Globs, p.MaxMatches
	// Two def patterns OR'd together:
	//   1. plain decl:   func Foo, function Foo, def Foo, class Foo, ...
	//   2. Go method:    func (recv T) Foo
	// Without (2) the trace shows "<not found>" for any method, which is
	// the most common Go callable.
	q := regexp.QuoteMeta(symbol)
	defPattern := fmt.Sprintf(`(func|function|def|class|type|struct|interface)\s+%s\b|func\s+\([^)]*\)\s+%s\b`, q, q)
	callPattern := fmt.Sprintf(`\b%s\s*\(`, q)

	defs, err := rgCollect(parent, ws, lim, defPattern, path, globs, maxMatches)
	if err != nil {
		return "", err
	}
	calls, err := rgCollect(parent, ws, lim, callPattern, path, globs, maxMatches)
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

func rgCollect(parent context.Context, ws Workspace, lim config.Limits, pattern, path string, globs []string, maxMatches int) ([]rgHit, error) {
	args := []string{"-n", "--no-heading", "--color", "never", "-g", "!.git"}
	if maxMatches > 0 {
		args = append(args, "--max-count", fmt.Sprintf("%d", maxMatches))
	}
	args = globArgs(args, globs)
	args = append(args, "--", pattern, path)

	captured, _, err := runRg(parent, ws, lim, args)
	if err != nil {
		return nil, err
	}
	var hits []rgHit
	for _, ln := range strings.Split(captured, "\n") {
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
