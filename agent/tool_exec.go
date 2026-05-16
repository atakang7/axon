package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

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
