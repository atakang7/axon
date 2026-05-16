package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/atakang7/axon/agent"
)

// customtool.go — adapter that turns a ToolConfig into a Tool the runtime
// can call. Today only `type: shell` is implemented. Same Tool shape used
// by built-ins, so the agent loop is indifferent to where a tool came from.

const defaultShellToolTimeout = 60 * time.Second

// buildCustomTool produces a Tool from a validated ToolConfig. The returned
// Tool's Fn renders the configured command template against the LLM-supplied
// args and runs it bound to the turn context.
func buildCustomTool(tc ToolConfig) (agent.Tool, error) {
	switch tc.Type {
	case "shell":
		return buildShellTool(tc)
	default:
		return agent.Tool{}, fmt.Errorf("unsupported tool type %q", tc.Type)
	}
}

func buildShellTool(tc ToolConfig) (agent.Tool, error) {
	cmdTpl, err := template.New("cmd").Funcs(shellFuncs).Parse(tc.Command)
	if err != nil {
		return agent.Tool{}, fmt.Errorf("parse command template: %w", err)
	}
	var cwdTpl *template.Template
	if tc.Cwd != "" {
		cwdTpl, err = template.New("cwd").Funcs(shellFuncs).Parse(tc.Cwd)
		if err != nil {
			return agent.Tool{}, fmt.Errorf("parse cwd template: %w", err)
		}
	}
	timeout := time.Duration(tc.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultShellToolTimeout
	}
	schema := tc.Schema
	if schema == nil {
		schema = map[string]any{"type": "object", "additionalProperties": false}
	}

	return agent.Tool{
		Name:        tc.Name,
		Description: tc.Description,
		Schema:      schema,
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := map[string]any{}
			if len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				if err := json.Unmarshal(raw, &args); err != nil {
					return "", fmt.Errorf("decode args: %w", err)
				}
			}
			cmd, err := renderTemplate(cmdTpl, args)
			if err != nil {
				return "", fmt.Errorf("render command: %w", err)
			}
			cwd := ""
			if cwdTpl != nil {
				cwd, err = renderTemplate(cwdTpl, args)
				if err != nil {
					return "", fmt.Errorf("render cwd: %w", err)
				}
			}
			return runShellCommand(ctx, cmd, cwd, timeout)
		},
	}, nil
}

func renderTemplate(t *template.Template, args map[string]any) (string, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, args); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// runShellCommand executes `sh -c <command>` with a timeout, capturing
// combined output. Bound to ctx so a turn-level cancellation kills the
// process group. Mirrors how ExecTool runs commands; we keep it separate
// here so custom tools aren't entangled with the larger Exec lifecycle
// (background shells, verify mode, etc.).
func runShellCommand(ctx context.Context, command, cwd string, timeout time.Duration) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("empty command after template rendering")
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c := exec.CommandContext(tctx, "sh", "-c", command)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if cwd != "" {
		c.Dir = cwd
	}
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	err := c.Run()
	body := strings.TrimRight(out.String(), "\n")
	if tctx.Err() == context.DeadlineExceeded {
		return body, fmt.Errorf("tool timed out after %s", timeout)
	}
	if err != nil {
		// Surface non-zero exits as tool errors but include captured output
		// so the model can see what the command actually said.
		if body == "" {
			return "", err
		}
		return body, err
	}
	return body, nil
}

// shellFuncs are the template funcs available inside a shell tool's
// command/cwd. Keep this set small and safe-by-default — anything we add
// here becomes part of the framework's stable surface.
var shellFuncs = template.FuncMap{
	// shellQuote wraps a value in single quotes, escaping any embedded
	// single quotes. Use for every interpolated value that becomes a
	// distinct shell word, to prevent command injection.
	//
	//   command: gh issue create --title {{.title | shellQuote}}
	"shellQuote": shellQuote,
}

func shellQuote(v any) string {
	s := fmt.Sprint(v)
	// 'foo' → 'foo'   'foo'bar' → 'foo'\''bar' (POSIX-portable trick).
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
