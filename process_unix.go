//go:build !windows

package axon

import (
	"os/exec"
	"syscall"
)

type processGroup struct{ pid int }

func startManagedProcess(cmd *exec.Cmd) (*processGroup, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &processGroup{pid: cmd.Process.Pid}, nil
}

func (g *processGroup) terminate(force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-g.pid, signal)
}

func (g *processGroup) close() {}

func processExitNote(err *exec.ExitError) string {
	if status, ok := err.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return "signaled: " + status.Signal().String()
	}
	return "exited"
}

func backgroundShellCommand(command string) *exec.Cmd {
	return exec.Command("sh", "-lc", command)
}

func foregroundShellCommand(command string) *exec.Cmd {
	if bash, err := exec.LookPath("bash"); err == nil {
		return exec.Command(bash, "-o", "pipefail", "-lc", command)
	}
	return backgroundShellCommand(command)
}

// persistentShell reports whether foreground commands run in bash, which is
// what carrying shell state between them needs.
func persistentShell() bool {
	_, err := exec.LookPath("bash")
	return err == nil
}

// ShellName names the shell the exec tool runs commands in, so a prompt can
// tell the model which syntax to use.
func ShellName() string {
	if _, err := exec.LookPath("bash"); err == nil {
		return "bash"
	}
	return "sh"
}
