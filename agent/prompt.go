package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// prompt.go — system prompt construction and project orientation.
//
// Layering, top of the prompt to bottom:
//
//  1. Role prompt — supplied by the embedder via Config.SystemPrompt.
//     Required. The runtime has no opinion of its own about what an
//     agent is; the role text is the entire "who am I" answer.
//  2. Built-in tool catalog — added by the runtime so every agent gets
//     a consistent description of the hands-and-legs tools.
//  3. Project orientation — fresh per-turn snapshot of the cwd file
//     tree (still useful for any agent that touches a workspace).
//
// Custom tool descriptions reach the model through the provider's tool
// schema, not the prompt.
// buildSystemPrompt composes the full system message:
//
//	[role prompt] + [built-in tool catalog] + [language/build probes] + [project orientation]
//
// rolePromptText is the agent's role — required, the runtime has no
// opinion of its own about what an agent is.
func buildSystemPrompt(s *Session, rolePromptText string) string {
	parts := []string{strings.TrimRight(rolePromptText, "\n")}
	parts = append(parts, builtinToolCatalog())
	if probes := runProbes(s.Cwd); probes != "" {
		parts = append(parts, probes)
	}
	parts = append(parts, projectOrientation(s))
	return strings.Join(parts, "\n\n")
}

// builtinToolCatalog lists the built-in tools the runtime adds to every
// agent. Terse on purpose — full mode docs live in each tool's
// Description field, which the provider sees via the tool schema.
func builtinToolCatalog() string {
	rows := []struct{ name, blurb string }{
		{toolRead, "Read files (skeleton / slice / full)."},
		{toolWrite, "Write files (create / overwrite / replace / insert)."},
		{toolExec, "Execute commands (run / verify; set run_in_background=true for servers and watchers)."},
		{toolBashOutput, "Read new output from a background shell (delta only)."},
		{toolKillShell, "Stop a background shell. Always clean up servers you started."},
		{toolSearch, "Search (literal / regex / trace)."},
		{toolTask, "Register a task objective."},
	}
	var b strings.Builder
	b.WriteString("# BUILT-IN TOOLS\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "\n%q — %s", r.name, r.blurb)
	}
	return b.String()
}

// projectOrientation produces a one-shot snapshot of the working directory,
// injected into the system prompt so the agent never has to spend a turn
// running `ls` or grepping for go.mod / package.json. Two-level shallow tree
// from cwd, skipping noisy directories. Capped so the prompt stays bounded
// regardless of repo size.
func projectOrientation(s *Session) string {
	cwd := s.Cwd
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	if cwd == "" {
		return "# PROJECT ORIENTATION\n(cwd unknown)"
	}

	const maxEntries = 200
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, "target": true,
		"dist": true, "build": true, ".next": true, ".venv": true, "venv": true,
		"__pycache__": true, ".idea": true, ".vscode": true,
	}

	type entry struct {
		path string
		dir  bool
	}
	var entries []entry
	count := 0

	var walk func(base string, depth int) bool
	walk = func(base string, depth int) bool {
		fis, err := os.ReadDir(base)
		if err != nil {
			return true
		}
		sort.Slice(fis, func(i, j int) bool {
			if fis[i].IsDir() != fis[j].IsDir() {
				return fis[i].IsDir()
			}
			return fis[i].Name() < fis[j].Name()
		})
		for _, fi := range fis {
			name := fi.Name()
			if strings.HasPrefix(name, ".") && name != ".github" && name != ".env.example" {
				if !fi.IsDir() {
					continue
				}
			}
			if fi.IsDir() && skipDirs[name] {
				continue
			}
			rel, _ := filepath.Rel(cwd, filepath.Join(base, name))
			entries = append(entries, entry{path: rel, dir: fi.IsDir()})
			count++
			if count >= maxEntries {
				return false
			}
			if fi.IsDir() && depth < 1 {
				if !walk(filepath.Join(base, name), depth+1) {
					return false
				}
			}
		}
		return true
	}
	complete := walk(cwd, 0)

	var b strings.Builder
	b.WriteString("# PROJECT ORIENTATION\n")
	fmt.Fprintf(&b, "cwd: %s\n", cwd)
	b.WriteString("This listing is authoritative — do NOT run `ls`, `find`, or search to discover what's here. Read or skeleton directly when you need contents.\n\n")
	if len(entries) == 0 {
		b.WriteString("(empty directory — 0 entries. Start creating files directly; do not probe.)\n")
		return b.String()
	}
	for _, e := range entries {
		if e.dir {
			fmt.Fprintf(&b, "  %s/\n", e.path)
		} else {
			fmt.Fprintf(&b, "  %s\n", e.path)
		}
	}
	if !complete {
		fmt.Fprintf(&b, "\n[listing truncated at %d entries — large repo; explore subdirectories with read or search if needed]\n", maxEntries)
	}
	return b.String()
}

