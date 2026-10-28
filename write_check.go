package axon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// After-write checks: every write result says whether the files it touched
// still parse and build, so a broken edit is seen in the same result that
// made it, not one or two requests later when a build happens to run. This is
// the cheapest verification there is: no model call, no decision to check.
//
// The checks are fast and local to what was written: Go files are gofmt'ed
// and their package is built (vetted, for a _test.go file); JSON is parsed;
// Python is compiled; JavaScript is syntax-checked by node. A language with
// no check gets none. A tool that is not installed is skipped.

// checkTimeout bounds one check; a slow build must not stall the write.
const checkTimeout = 45 * time.Second

// checkOutputLines bounds what a failed check reports.
const checkOutputLines = 20

// afterWrite formats and checks the files a write touched and returns the
// line to append to its result, or "" when nothing applies.
func afterWrite(ctx context.Context, paths []string) string {
	var lines []string
	goDirs := map[string][]string{} // package dir -> files written there
	for _, p := range paths {
		switch strings.ToLower(filepath.Ext(p)) {
		case ".go":
			if msg := goSyntax(p); msg != "" {
				lines = append(lines, msg)
				continue
			}
			gofmtFile(ctx, p)
			goDirs[filepath.Dir(p)] = append(goDirs[filepath.Dir(p)], p)
		case ".json":
			if msg := jsonSyntax(p); msg != "" {
				lines = append(lines, msg)
			}
		case ".py":
			if msg := runCheck(ctx, filepath.Dir(p), pythonCommand(), "-m", "py_compile", p); msg != "" {
				lines = append(lines, "python: "+msg)
			}
		case ".js", ".mjs", ".cjs":
			if msg := runCheck(ctx, filepath.Dir(p), "node", "--check", p); msg != "" {
				lines = append(lines, "node --check: "+msg)
			}
		}
	}
	dirs := make([]string, 0, len(goDirs))
	for d := range goDirs {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	built := false
	for _, d := range dirs {
		msg, ran := goBuild(ctx, d, goDirs[d])
		built = built || ran
		if msg != "" {
			lines = append(lines, msg)
		}
	}
	if len(lines) == 0 {
		if built {
			return "check: ok (gofmt, go build)"
		}
		return ""
	}
	return "check: FAILED — fix this before moving on\n" + strings.Join(lines, "\n")
}

func goSyntax(path string) string {
	src, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if _, err := parser.ParseFile(token.NewFileSet(), path, src, parser.AllErrors); err != nil {
		return "go syntax: " + firstLines(err.Error(), checkOutputLines)
	}
	return ""
}

func gofmtFile(ctx context.Context, path string) {
	bin, err := exec.LookPath("gofmt")
	if err != nil {
		goBin, gerr := exec.LookPath("go")
		if gerr != nil {
			return
		}
		bin = filepath.Join(filepath.Dir(goBin), "gofmt")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, bin, "-w", path).Run()
}

// goBuild compiles the package in dir; a written test file vets the package
// with its tests, which compiles them without running them. It reports
// whether a build ran at all.
func goBuild(ctx context.Context, dir string, files []string) (string, bool) {
	if _, err := exec.LookPath("go"); err != nil || !inGoModule(dir) {
		return "", false
	}
	args := []string{"build", "-o", os.DevNull, "."}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			args = []string{"vet", "."}
			break
		}
	}
	if msg := runCheck(ctx, dir, "go", args...); msg != "" {
		return "go " + args[0] + " (" + dir + "): " + msg, true
	}
	return "", true
}

func inGoModule(dir string) bool {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return false
		}
		d = parent
	}
}

func jsonSyntax(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		if se, ok := err.(*json.SyntaxError); ok {
			line := 1 + bytes.Count(data[:se.Offset], []byte("\n"))
			return fmt.Sprintf("json (%s): line %d: %v", path, line, err)
		}
		return fmt.Sprintf("json (%s): %v", path, err)
	}
	return ""
}

// pythonCommand is the first Python that actually runs. On Windows
// "python3" is often the Microsoft Store stub, which exists on PATH and only
// prints an install hint; a check run with it reported that hint as a failure.
func pythonCommand() string {
	pythonOnce.Do(func() {
		for _, name := range []string{"python3", "python"} {
			if _, err := exec.LookPath(name); err != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := exec.CommandContext(ctx, name, "-c", "import sys").Run()
			cancel()
			if err == nil {
				pythonName = name
				return
			}
		}
	})
	return pythonName
}

var (
	pythonOnce sync.Once
	pythonName string
)

// runCheck runs one check command and returns its output when it failed, ""
// when it passed or could not run at all.
func runCheck(ctx context.Context, dir, name string, args ...string) string {
	if name == "" {
		return ""
	}
	if _, err := exec.LookPath(name); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return ""
	}
	if ctx.Err() != nil {
		return "" // too slow to be a check; the oracle will tell
	}
	if _, ok := err.(*exec.ExitError); !ok {
		return ""
	}
	return firstLines(strings.TrimSpace(string(out)), checkOutputLines)
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n[%d more lines]", len(lines)-n)
}
