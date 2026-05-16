package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Probe is one shell command run once at session startup. Its trimmed stdout
// is spliced into the system prompt as "label: <output>". Probes are for
// session-stable system facts (os, user, git remote) — not per-turn state.
type Probe struct {
	Label   string `json:"label"`
	Command string `json:"command"`
}

// defaultProbes covers the facts the agent would otherwise waste a turn
// running ls/uname/whoami to discover. Kept short on purpose — every line
// here costs tokens on every model call for the rest of the session.
var defaultProbes = []Probe{
	{"pwd", "pwd"},
	{"user", "whoami"},
	{"shell", "basename \"$SHELL\""},
	{"os", "uname -srm"},
	{"date", "date '+%Y-%m-%d %H:%M:%S %Z'"},
	{"git_branch", "git rev-parse --abbrev-ref HEAD 2>/dev/null"},
	{"git_remote", "git config --get remote.origin.url 2>/dev/null"},
	{"git_status", "git status --short 2>/dev/null | head -20"},
}

const (
	probeTimeout     = 1500 * time.Millisecond
	probeOutputLimit = 2000
)

func probesPath() string {
	if path := envString("AXON_CONTEXT_PROBES_PATH"); path != "" {
		return path
	}
	return filepath.Join(configDir(), "context.json")
}

func loadProbes() []Probe {
	path := probesPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultProbes
	}
	var probes []Probe
	if err := json.Unmarshal(data, &probes); err != nil {
		return defaultProbes
	}
	return probes
}

// runProbes executes every probe with a hard per-probe timeout, in the
// session cwd. Failures and empty outputs are silently dropped — a broken
// probe must never break startup or pollute the prompt with error noise.
func runProbes(cwd string) string {
	probes := loadProbes()
	if len(probes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# SYSTEM CONTEXT\n")
	b.WriteString("Session-stable facts probed at startup. Do NOT re-run these commands to verify them.\n\n")
	any := false
	for _, p := range probes {
		out := runProbe(p.Command, cwd)
		if out == "" {
			continue
		}
		any = true
		if strings.Contains(out, "\n") {
			fmt.Fprintf(&b, "%s:\n%s\n", p.Label, indent(out, "  "))
		} else {
			fmt.Fprintf(&b, "%s: %s\n", p.Label, out)
		}
	}
	if !any {
		return ""
	}
	return b.String()
}

func runProbe(command, cwd string) string {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	s := strings.TrimRight(string(out), "\n\t ")
	if len(s) > probeOutputLimit {
		s = s[:probeOutputLimit] + "\n[truncated]"
	}
	return s
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
