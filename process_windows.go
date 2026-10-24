package axon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A job owns the shell and its descendants. Closing the job kills remaining
// children, including children whose parent shell has already exited.
type processGroup struct {
	mu  sync.Mutex
	job windows.Handle
}

func startManagedProcess(cmd *exec.Cmd) (*processGroup, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create process job: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("configure process job: %w", err)
	}
	// Start suspended so no child can escape before job assignment.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		windows.CloseHandle(process)
	}
	if err == nil {
		err = resumeProcess(uint32(cmd.Process.Pid))
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		windows.CloseHandle(job)
		return nil, fmt.Errorf("attach process job: %w", err)
	}
	return &processGroup{job: job}, nil
}

func resumeProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		return err
	}
	return fmt.Errorf("primary thread not found for pid %d: %w", pid, err)
}

func (g *processGroup) terminate(force bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job == 0 {
		return nil
	}
	// Windows has no POSIX SIGTERM. Both cancellation modes terminate the job.
	return windows.TerminateJobObject(g.job, 1)
}

func (g *processGroup) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job != 0 {
		windows.CloseHandle(g.job)
		g.job = 0
	}
}

func processExitNote(err *exec.ExitError) string { return "exited" }

func backgroundShellCommand(command string) *exec.Cmd { return foregroundShellCommand(command) }

func foregroundShellCommand(command string) *exec.Cmd {
	if bash := gitBash(); bash != "" {
		return exec.Command(bash, "-o", "pipefail", "-lc", command)
	}
	return exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
}

// gitBash is Git for Windows Bash, never the legacy Windows WSL bash launcher,
// or "" when it is not installed.
func gitBash() string {
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs")} {
		path := filepath.Join(root, "Git", "bin", "bash.exe")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// ShellName names the shell the exec tool runs commands in, so a prompt can
// tell the model which syntax to use.
func ShellName() string {
	if gitBash() != "" {
		return "Git Bash (bash syntax; Windows paths work as /c/Users/...)"
	}
	return "Windows PowerShell (PowerShell syntax, Windows paths)"
}
