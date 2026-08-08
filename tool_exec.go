package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// EXEC — non-interactive shell command. LLM controls tail size.
// ---------------------------------------------------------------------------

const execDescription = `Run a shell command. tail_lines is required.
Set run_in_background=true for any command that *might* wait — servers, watchers, HTTP clients (curl/wget against any service, including ones you just started), database clients, anything reading stdin or a socket, anything connecting to a host you don't fully control. The rule is the chance of hanging, not the certainty: if you'd be surprised by either outcome, go background. Foreground is for commands you know terminate on their own (build, vet, test, format, file I/O, deterministic CPU work). Background returns a shell_id immediately; use bash_output to read logs and kill_shell to stop.
Stdin is always /dev/null — interactive commands (prompts, REPLs, password reads) WILL hang. Use non-interactive flags (-y, --yes, --non-interactive) instead.`

type execInput struct {
	Command         string `json:"command"`
	TailLines       int    `json:"tail_lines"`
	ExpectedOutcome string `json:"expected_outcome"`
	Dir             string `json:"dir"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	RunInBackground bool   `json:"run_in_background"`
}

func parseAndValidateExecInput(raw json.RawMessage, ws Workspace, lim Limits) (*execInput, string, error) {
	var p execInput
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "", err
	}
	resolvedDir := ws.Dir()
	if strings.TrimSpace(p.Dir) != "" {
		resolvedDir = ws.ResolvePath(p.Dir)
	}
	if strings.TrimSpace(p.Command) == "" {
		return nil, "", fmt.Errorf("command is required")
	}
	if !p.RunInBackground && p.TailLines <= 0 {
		return nil, "", fmt.Errorf("tail_lines is required and must be > 0")
	}
	if max := lim.ExecMaxTailLines; p.TailLines > max {
		p.TailLines = max
	}
	if p.TimeoutSeconds <= 0 {
		p.TimeoutSeconds = int(lim.ExecTimeout.Seconds())
	}
	if max := int(lim.ExecMaxTimeout.Seconds()); p.TimeoutSeconds > max {
		p.TimeoutSeconds = max
	}
	return &p, resolvedDir, nil
}

func ExecTool(ws Workspace, shells *BackgroundShells, lim Limits) Tool {
	return Tool{
		Name:        toolExec,
		Description: execDescription,
		Schema: obj("object", props{
			"command":           strSchema("Shell command."),
			"tail_lines":        intSchema("Last N lines to keep. Required. Ignored when run_in_background=true."),
			"expected_outcome":  strSchema("What success looks like. Optional but enables structured failure diagnosis."),
			"dir":               strSchema("Optional working directory override."),
			"timeout_seconds":   intSchema(fmt.Sprintf("Default %d. Ignored when run_in_background=true.", int(lim.ExecTimeout.Seconds()))),
			"run_in_background": boolSchema("Spawn detached and return a shell_id immediately. Use for servers, watchers, anything long-running. Default false."),
		}, []string{"command"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			p, resolvedDir, err := parseAndValidateExecInput(raw, ws, lim)
			if err != nil {
				return "", err
			}

			if p.RunInBackground {
				sh, err := shells.start(p.Command, resolvedDir)
				if err != nil {
					return "", err
				}
				return formatBgStart(sh), nil
			}

			return runForegroundProcess(ctx, p, resolvedDir, lim.ExecOutputBytes)
		},
	}
}

// killGrace bounds how long we wait for a killed command's output copier to
// finish before giving up on it and returning what we captured.
const killGrace = 2 * time.Second

func runForegroundProcess(ctx context.Context, p *execInput, resolvedDir string, outputBytes int) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.Command("sh", "-lc", p.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if resolvedDir != "" {
		cmd.Dir = resolvedDir
	}
	if dn, err := os.Open(os.DevNull); err == nil {
		cmd.Stdin = dn
		defer dn.Close()
	}

	buf := &limitBuf{limit: outputBytes}
	cmd.Stdout = buf
	cmd.Stderr = buf

	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var runErr error
	abandoned := false
	select {
	case runErr = <-done:
	case <-runCtx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		// Do not wait on Wait indefinitely. Because Stdout is an io.Writer
		// rather than a file, os/exec pipes the output through a copier
		// goroutine, and Wait blocks until that copier sees EOF. A grandchild
		// that escaped the process group (anything calling setsid, or a
		// daemonising server) still holds the write end open, so EOF never
		// arrives and Wait never returns. Blocking here would hang the turn
		// forever with no way out but killing the agent.
		select {
		case runErr = <-done:
		case <-time.After(killGrace):
			abandoned = true
			runErr = fmt.Errorf("killed, but a child process is still holding the output pipe open")
		}
	}

	code := 0
	note := ""
	if runErr != nil {
		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			code = -1
			note = "timed out"
		case abandoned:
			code = -1
			note = "timed out; killed, but an escaped child still holds the output pipe — output may be incomplete"
		case ctx.Err() != nil:
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

	captured, truncated := buf.snapshot()
	tailed, hidden := tailN(captured, p.TailLines)
	return formatExec(p.Command, cmd.Dir, code, p.ExpectedOutcome, tailed, hidden, truncated, note), nil
}

const bashOutputDescription = `Read new output from a background shell since the last read. Status is "running" or the exit summary. Returns only the delta — calling this in a poll loop is cheap; rereading the same bytes is not.
  - tail_lines: optional. Keep only the last N lines of the delta. Useful for chatty servers.
  - max_bytes: optional. Cap returned bytes (tail kept). Default ~32 KiB; offset still advances past dropped bytes so the next call continues from "now."`

func BashOutputTool(shells *BackgroundShells, lim Limits) Tool {
	return Tool{
		Name:        toolBashOutput,
		Description: bashOutputDescription,
		Schema: obj("object", props{
			"shell_id":   strSchema("Background shell handle, e.g. bash_1."),
			"tail_lines": intSchema("Optional. Keep only the last N lines of the new delta."),
			"max_bytes":  intSchema("Optional. Cap returned bytes (tail kept). Default ~32 KiB."),
		}, []string{"shell_id"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				ShellID   string `json:"shell_id"`
				TailLines int    `json:"tail_lines"`
				MaxBytes  int    `json:"max_bytes"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			sh, ok := shells.get(p.ShellID)
			if !ok {
				return "", fmt.Errorf("unknown shell_id: %s", p.ShellID)
			}
			cap := p.MaxBytes
			if cap <= 0 {
				cap = lim.BashOutputMaxBytes
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

func KillShellTool(shells *BackgroundShells) Tool {
	return Tool{
		Name:        toolKillShell,
		Description: killShellDescription,
		Schema: obj("object", props{
			"shell_id": strSchema("Background shell handle, e.g. bash_1."),
		}, []string{"shell_id"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				ShellID string `json:"shell_id"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			sh, ok := shells.get(p.ShellID)
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
