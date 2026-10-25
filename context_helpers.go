package axon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Helpers shared by the context view, checkpoints and the goal contract.

// contextTokens converts a size in bytes with the ratio the meter holds now.
// A soft policy unit: no safety margin, unlike the hard input budget.
func contextTokens(n int) int {
	return int(float64(n) / meter.bytesPerToken(""))
}

// protectedIDs returns the newest user and assistant message: losing either
// leaves the agent unable to tell what it was just asked or what it just said.
func protectedIDs(s *Session) map[string]bool {
	protected := map[string]bool{}

	var haveUser, haveAssistant bool
	for i := len(s.Messages) - 1; i >= 0 && !(haveUser && haveAssistant); i-- {
		m := s.Messages[i]
		if m.ID == "" || m.Parked {
			continue
		}

		switch {
		case m.Role == "user" && !haveUser:
			protected[m.ID] = true
			haveUser = true
		case m.Role == "assistant" && !haveAssistant:
			protected[m.ID] = true
			haveAssistant = true
		}
	}

	return protected
}

// gist is a block's one-line stand-in: the tool's call when there is one, else
// the first line of its text.
func gist(s *Session, id string) string {
	uses := toolUses(s)
	for _, m := range s.Messages {
		if m.ID != id {
			continue
		}
		if m.Role == "tool" {
			if u, ok := uses[m.ToolCallID]; ok {
				if g := callGist(u, m); g != "" {
					return g
				}
			}
		}
		label := m.Role
		if m.ToolName != "" {
			label = m.ToolName
		}
		first := truncateRunes(firstLine(m.Content), 80)
		if first == "" {
			return "(" + label + ")"
		}
		return label + ": " + first
	}
	return "(unknown block)"
}

// callGist names what a tool call did, from its own arguments: "read
// auth/token.go 1-200", "exec go test ./... → exit 1".
func callGist(u toolUse, m Msg) string {
	switch u.name {
	case toolRead:
		paths := readPaths(u)
		if len(paths) == 0 {
			return ""
		}
		g := "read " + strings.Join(paths, ", ")
		if off, ok := u.args["offset"].(float64); ok && len(paths) == 1 {
			if lim, ok := u.args["limit"].(float64); ok {
				g += fmt.Sprintf(" lines %d-%d", int(off)+1, int(off+lim))
			}
		}
		return truncateRunes(g, 160)
	case toolSearch:
		return truncateRunes("search "+oneLine(argString(u.args, "pattern"), 80)+" in "+orDot(argString(u.args, "path")), 160)
	case toolExec:
		g := "exec " + oneLine(argString(u.args, "command"), 100)
		if code := exitCode(m.Content); code != "" {
			g += " → exit " + code
		}
		return truncateRunes(g, 160)
	case toolExplore:
		return truncateRunes("explore "+oneLine(argString(u.args, "question"), 120), 160)
	}
	return ""
}

func orDot(p string) string {
	if p == "" {
		return "."
	}
	return p
}

// exitCode reads the "exit_code: N" line exec puts near the top of a result.
func exitCode(content string) string {
	for _, line := range strings.SplitN(content, "\n", 10) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "exit_code:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// firstLine is a block's first non-empty line after its "[#mN]" stamp, capped.
func firstLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || (strings.HasPrefix(line, "[#") && strings.HasSuffix(line, "]")) {
			continue
		}
		if r := []rune(line); len(r) > 160 {
			line = string(r[:160])
		}
		return line
	}
	return ""
}

// groundQuote finds quote in content and returns the exact source text. Models
// routinely normalize whitespace (tabs to spaces, CRLF, collapsed runs) when
// copying, so a run of whitespace in the quote matches any run in the source.
// Every non-space character must still appear in order, contiguously.
func groundQuote(content, quote string) (string, bool) {
	if quote != "" && strings.Contains(content, quote) {
		return quote, true
	}
	loc := quoteIndex(content, quote)
	if loc == nil {
		return "", false
	}
	match := content[loc[0]:loc[1]]
	return match, len([]rune(match)) <= 600
}

// quoteIndex is groundQuote's match position, or nil.
func quoteIndex(content, quote string) []int {
	if quote != "" {
		if i := strings.Index(content, quote); i >= 0 {
			return []int{i, i + len(quote)}
		}
	}
	words := strings.Fields(quote)
	if len(words) == 0 {
		return nil
	}
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	re, err := regexp.Compile(strings.Join(words, `\s+`))
	if err != nil {
		return nil
	}
	return re.FindStringIndex(content)
}

// unresolvedToolFailures are failed results whose exact call has not
// succeeded since. An unrelated successful command is not resolution, so they
// stay in context until it is.
func unresolvedToolFailures(s *Session) map[string]bool {
	signatures := map[string]string{}
	for _, m := range s.Messages {
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				signatures[tc.ID] = tc.Function.Name + "\x00" + tc.Function.Arguments
			}
		}
	}
	succeeded := map[string]bool{}
	protected := map[string]bool{}
	for i := len(s.Messages) - 1; i >= 0; i-- {
		m := s.Messages[i]
		if m.Role != "tool" {
			continue
		}
		key := signatures[m.ToolCallID]
		failed := m.ToolFailed
		knownSuccess := !failed
		if m.ToolName == "exec" || m.ToolName == "bash_output" {
			knownSuccess = false
			for _, line := range strings.SplitN(m.Content, "\n", 10) {
				var code int
				if n, _ := fmt.Sscanf(line, "exit_code: %d", &code); n == 1 {
					failed = failed || code != 0
					knownSuccess = !failed
					break
				}
			}
		}
		if failed && (key == "" || !succeeded[key]) {
			protected[m.ID] = true
		}
		if knownSuccess && key != "" {
			succeeded[key] = true
		}
	}
	return protected
}

// saveSessionAtomic writes the session through a temporary file and a rename,
// so a crash mid-write cannot leave a half-written session behind.
func saveSessionAtomic(s *Session) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".axon-session-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.path)
}
