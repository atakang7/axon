package axon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// These helpers use native Windows processes, independent of Bash or WSL.
func TestWindowsProcessHelper(t *testing.T) {
	if os.Getenv("AXON_WINDOWS_PROCESS_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "child" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$", "--", "child")
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	fmt.Printf("%d\n", child.Process.Pid)
	if mode == "parent-exit" {
		os.Exit(0)
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestWindowsJobTerminatesProcessTree(t *testing.T) {
	for _, mode := range []string{"parent-wait", "parent-exit"} {
		t.Run(mode, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "child.pid")
			log, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$", "--", mode)
			cmd.Env = append(os.Environ(), "AXON_WINDOWS_PROCESS_HELPER=1")
			cmd.Stdout = log
			group, err := startManagedProcess(cmd)
			if err != nil {
				t.Fatal(err)
			}
			defer group.close()
			pid := 0
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				data, _ := os.ReadFile(logPath)
				pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
				if pid > 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				t.Fatal("child did not start")
			}
			child, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
			if err != nil {
				t.Fatal(err)
			}
			defer windows.CloseHandle(child)
			if mode == "parent-wait" {
				if err := group.terminate(true); err != nil {
					t.Fatal(err)
				}
				_ = cmd.Wait()
			} else {
				if err := cmd.Wait(); err != nil {
					t.Fatal(err)
				}
				group.close()
			}
			status, err := windows.WaitForSingleObject(child, 5000)
			if err != nil || status != windows.WAIT_OBJECT_0 {
				t.Fatalf("child survived: wait=%v err=%v", status, err)
			}
		})
	}
}

func TestWindowsForegroundCommandUsesNativeShell(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := "echo windows_native_sentinel"
	result, err := runForegroundProcess(ctx, &execInput{Command: command, TailLines: 5, TimeoutSeconds: 5}, t.TempDir(), 4096, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "windows_native_sentinel") || !strings.Contains(result, "exit_code: 0") {
		t.Fatal(result)
	}
}
