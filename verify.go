package axon

import (
	"fmt"
	"strings"
)

// Verification is the one behaviour the loop itself enforces: a turn that
// changed files ends only after something checked them, or after one note
// asking for that. It is computed from the log, never taken from the
// model's word, and replaces the goal tool's criteria ceremony, which cost
// up to half the requests of a task (eval, 2026-10-28). See BEHAVIORS.md.

// trivialCommands only look; running them after a change checks nothing.
var trivialCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "grep": true, "rg": true, "find": true,
	"echo": true, "printf": true, "pwd": true, "wc": true, "which": true, "type": true, "tree": true,
	"stat": true, "file": true, "du": true, "cd": true, "sed": true, "awk": true, "sort": true,
	"diff": true, "true": true, "sleep": true, "date": true, "env": true, "export": true,
}

// isCheckCommand reports whether command does more than look: it builds,
// tests or runs something.
func isCheckCommand(command string) bool {
	for _, seg := range strings.FieldsFunc(command, func(r rune) bool { return r == '&' || r == ';' || r == '|' || r == '\n' }) {
		f := strings.Fields(seg)
		if len(f) == 0 {
			continue
		}
		name := f[0]
		if name == "git" {
			continue
		}
		if !trivialCommands[name] {
			return true
		}
	}
	return false
}

type verification struct {
	wrote    bool   // the open run changed files
	state    string // "verified", "failed" or "unverified"
	evidence string // block id of the deciding check
	command  string
}

// verification looks at the open run: its last file change and the last
// check that ran after it.
func (s *Session) verification() verification {
	runs := sessionRuns(s)
	if len(runs) == 0 {
		return verification{}
	}
	r := runs[len(runs)-1]
	uses := toolUses(s)
	last := -1
	for i := r.start + 1; i < len(s.Messages); i++ {
		m := s.Messages[i]
		if m.Role == "tool" && !m.ToolFailed && isWriteTool(uses[m.ToolCallID].name) {
			last = i
		}
	}
	if last < 0 {
		return verification{}
	}
	v := verification{wrote: true, state: "unverified"}
	for i := last + 1; i < len(s.Messages); i++ {
		m := s.Messages[i]
		u := uses[m.ToolCallID]
		if m.Role != "tool" || u.name != toolExec {
			continue
		}
		cmd := argString(u.args, "command")
		if !isCheckCommand(cmd) {
			continue
		}
		v.evidence, v.command = m.ID, cmd
		if !m.ToolFailed && execExitCode(m.Content) == 0 {
			v.state = "verified"
		} else {
			v.state = "failed"
		}
	}
	return v
}

// verifyNote is the note for a turn about to end with its change unchecked,
// or "" when there is nothing to say.
func (v verification) note() string {
	switch {
	case !v.wrote || v.state == "verified":
		return ""
	case v.state == "failed":
		return fmt.Sprintf("[runtime note: the last check after your change failed (#%s, `%s`). Fix it and run it again, or say plainly in your answer that it fails and why. Do not repeat your previous answer.]", v.evidence, oneLine(v.command, 120))
	default:
		return "[runtime note: you changed files, but nothing has run since your last change to check them. Run the check that proves the change (the project's tests, a build, or the program itself) before answering. If it cannot run here, say so plainly in your answer. Do not repeat your previous answer.]"
	}
}

// stateBlock is sent at the end of every request: the plan and whether the
// current change is checked. Empty when there is neither.
func (s *Session) stateBlock() string {
	var parts []string
	if len(s.Todos) > 0 {
		parts = append(parts, "[plan]\n"+s.todoLines())
	}
	if v := s.verification(); v.wrote {
		line := "[change] files changed in this request; "
		switch v.state {
		case "verified":
			line += fmt.Sprintf("checked after the last change by `%s` (#%s): passed", oneLine(v.command, 80), v.evidence)
		case "failed":
			line += fmt.Sprintf("the last check `%s` (#%s) failed", oneLine(v.command, 80), v.evidence)
		default:
			line += "not checked since the last change"
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n")
}
