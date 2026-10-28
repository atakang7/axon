package axon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The workspace brief is what a developer knows the moment they open a
// project: its layout, how it builds and tests, and the instructions its
// authors left for agents (AGENTS.md, CLAUDE.md). It is computed once when
// the session's system message is built, without a model, so it sits in the
// cached prefix of every request and the first request can decide instead of
// spending a dozen round trips listing directories.

// briefInstructionBytes caps the project instructions carried in the brief.
const briefInstructionBytes = 8 << 10

// instructionFiles are read, in order, as the project's own instructions;
// the first one present is used.
var instructionFiles = []string{"AGENTS.md", "CLAUDE.md", ".cortex/instructions.md", ".github/copilot-instructions.md"}

// workspaceBrief describes dir, or returns "" when there is nothing to say.
func workspaceBrief(dir string) string {
	if dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Workspace\n")
	fmt.Fprintf(&b, "Root: %s (a snapshot from the start of the session; files may have changed since)\n", dir)

	b.WriteString("\nTop level:\n")
	var dirs, files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if skipDirs[name] || strings.HasPrefix(name, ".") {
				continue
			}
			sub, _ := os.ReadDir(filepath.Join(dir, name))
			dirs = append(dirs, fmt.Sprintf("%s/ (%d)", name, len(sub)))
		} else {
			files = append(files, name)
		}
	}
	const maxNames = 40
	if len(dirs) > maxNames {
		dirs = append(dirs[:maxNames], fmt.Sprintf("… %d more", len(dirs)-maxNames))
	}
	if len(files) > maxNames {
		files = summarizeFiles(files, maxNames)
	}
	if len(dirs) > 0 {
		b.WriteString("  dirs: " + strings.Join(dirs, ", ") + "\n")
	}
	if len(files) > 0 {
		b.WriteString("  files: " + strings.Join(files, ", ") + "\n")
	}

	if cmds := projectCommands(dir); len(cmds) > 0 {
		b.WriteString("\nHow it builds and tests (detected from its files; prefer what the instructions below say):\n")
		for _, c := range cmds {
			b.WriteString("  " + c + "\n")
		}
	}

	for _, name := range instructionFiles {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || len(strings.TrimSpace(string(data))) == 0 {
			continue
		}
		text := strings.TrimSpace(string(data))
		if len(text) > briefInstructionBytes {
			text = text[:briefInstructionBytes] + fmt.Sprintf("\n[… cut; read %s for the rest]", name)
		}
		fmt.Fprintf(&b, "\nProject instructions (%s), from the repository's authors — follow them:\n%s\n", name, text)
		break
	}
	return strings.TrimRight(b.String(), "\n")
}

// summarizeFiles keeps the files that are not source (READMEs, manifests,
// configs) by name and counts the rest by extension: a flat package of a
// hundred source files says what it is in one line.
func summarizeFiles(files []string, max int) []string {
	var named []string
	byExt := map[string]int{}
	for _, f := range files {
		if declPattern(f) != nil && !strings.HasSuffix(strings.ToLower(f), ".md") {
			byExt[strings.ToLower(filepath.Ext(f))]++
			continue
		}
		named = append(named, f)
	}
	if len(named) > max {
		named = append(named[:max], fmt.Sprintf("… %d more", len(named)-max))
	}
	exts := make([]string, 0, len(byExt))
	for e := range byExt {
		exts = append(exts, e)
	}
	sort.Strings(exts)
	for _, e := range exts {
		named = append(named, fmt.Sprintf("%d %s files", byExt[e], e))
	}
	return named
}

var makeTarget = regexp.MustCompile(`(?m)^([A-Za-z][\w.-]*):`)

// hasPythonFiles reports whether dir or one of its direct subdirectories
// holds Python source.
func hasPythonFiles(dir string) bool {
	for _, pattern := range []string{"*.py", "*/*.py"} {
		if m, _ := filepath.Glob(filepath.Join(dir, pattern)); len(m) > 0 {
			return true
		}
	}
	return false
}

// projectCommands lists the build and test commands a project's files name.
func projectCommands(dir string) []string {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	var out []string
	if has("go.mod") || has("go.work") {
		out = append(out, "go: go build ./... ; go test ./... ; go vet ./...")
	}
	if has("package.json") {
		runner := "npm run"
		switch {
		case has("pnpm-lock.yaml"):
			runner = "pnpm"
		case has("yarn.lock"):
			runner = "yarn"
		case has("bun.lockb") || has("bun.lock"):
			runner = "bun run"
		}
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil && json.Unmarshal(data, &pkg) == nil && len(pkg.Scripts) > 0 {
			names := make([]string, 0, len(pkg.Scripts))
			for n := range pkg.Scripts {
				names = append(names, n)
			}
			sort.Strings(names)
			var parts []string
			for _, n := range names {
				if len(parts) == 12 {
					parts = append(parts, "…")
					break
				}
				parts = append(parts, fmt.Sprintf("%s %s (%s)", runner, n, oneLine(pkg.Scripts[n], 60)))
			}
			out = append(out, "node: "+strings.Join(parts, "; "))
		}
	}
	if has("gradlew") || has("build.gradle") || has("build.gradle.kts") {
		out = append(out, "gradle: ./gradlew assembleDebug ; ./gradlew test (slow: run in the background or with a long timeout)")
	}
	if has("Cargo.toml") {
		out = append(out, "rust: cargo build ; cargo test")
	}
	if py := pythonCommand(); py != "" {
		// The interpreter that runs here, by name: an oracle written with
		// "python" on a machine that only has python3 could never run.
		switch {
		case has("pyproject.toml") || has("setup.py") || has("pytest.ini") || has("requirements.txt"):
			out = append(out, "python: "+py+" -m pytest (if the project uses pytest)")
		case hasPythonFiles(dir):
			out = append(out, "python: run it with "+py+" (\"python\" may not exist here); tests: "+py+" -m unittest discover")
		}
	}
	if has("pom.xml") {
		out = append(out, "maven: mvn -q compile ; mvn -q test")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "Makefile")); err == nil {
		var targets []string
		for _, m := range makeTarget.FindAllStringSubmatch(string(data), -1) {
			if len(targets) < 15 && !strings.HasPrefix(m[1], ".") {
				targets = append(targets, m[1])
			}
		}
		if len(targets) > 0 {
			out = append(out, "make: "+strings.Join(targets, ", "))
		}
	}
	return out
}
