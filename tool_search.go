package axon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ---------------------------------------------------------------------------
// SEARCH — multi-file content. Two modes.
// ---------------------------------------------------------------------------

const searchDescription = `Search across files.
  - literal: exact string.
  - regex: regex pattern.
To find where a symbol is defined, search for its declaration in the syntax of
the language it is written in — you know the language, so you can match it
exactly instead of guessing.`

func SearchTool(ws Workspace, lim Limits) Tool {
	return Tool{
		Name:        toolSearch,
		Description: searchDescription,
		Schema: obj("object", props{
			"query":          strSchema("Text or regex pattern, depending on mode."),
			"mode":           enumSchema("literal | regex. Optional; defaults to literal.", searchLiteral, searchRegex),
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
			default:
				return "", fmt.Errorf("unknown mode %q: literal | regex", p.Mode)
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

// errNoRipgrep is returned when rg is missing. Search is nothing without it,
// so the message names the fix rather than the symptom.
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
// It is kept separate from query construction so that the timeout, the output
// cap and the exit-status interpretation are decided in one place regardless
// of what is being searched for.
func runRg(parent context.Context, ws Workspace, lim Limits, args []string) (out string, truncated bool, err error) {
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
func runRipgrep(parent context.Context, ws Workspace, lim Limits, p searchInput, literal bool) (string, error) {
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
