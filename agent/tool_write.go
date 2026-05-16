package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// WRITE — five modes. Each mode has a deterministic contract.
// ---------------------------------------------------------------------------

const writeDescription = `Write to a file.
  - save: set the file's full contents. Creates if absent, replaces if present. Use this whenever you have the whole file in hand — do not check existence first.
  - replace_string: replace one exact occurrence of old_str.
  - replace_lines: replace lines [start_line, end_line].
  - insert_at_line: insert before start_line (1-based).

Writes are atomic (tmp + rename) and reversible via /undo. A formatter runs after every write. For brace languages emit content flat (no indentation). For whitespace-significant languages (Python, YAML) emit indentation correctly.`

func WriteTool(s *Session) Tool {
	return Tool{
		Name:        toolWrite,
		Description: writeDescription,
		Schema: obj("object", props{
			"path":       strSchema("Relative or absolute file path."),
			"mode":       enumSchema("save | replace_string | replace_lines | insert_at_line. Required.", writeSave, writeReplaceStr, writeReplaceLn, writeInsertAt),
			"content":    strSchema("New content. Required for all modes."),
			"old_str":    strSchema("Exact text to replace. Required when mode=replace_string."),
			"start_line": intSchema("1-based start line. Required when mode=replace_lines or insert_at_line."),
			"end_line":   intSchema("1-based end line, inclusive. Required when mode=replace_lines."),
			"reason":     reasonField(),
		}, []string{"path", "mode", "content", "reason"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p struct {
				Path      string `json:"path"`
				Mode      string `json:"mode"`
				Content   string `json:"content"`
				OldStr    string `json:"old_str"`
				StartLine int    `json:"start_line"`
				EndLine   int    `json:"end_line"`
				Reason    string `json:"reason"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Path) == "" {
				return "", fmt.Errorf("path is required")
			}
			abs := s.ResolvePath(p.Path)
			switch p.Mode {
			case writeSave:
				return writeSaveMode(s, abs, p.Content)
			case writeReplaceStr:
				if p.OldStr == "" {
					return "", fmt.Errorf("old_str is required for mode=replace_string (use overwrite if you mean to replace the whole file)")
				}
				return writeReplaceStringMode(s, abs, p.OldStr, p.Content)
			case writeReplaceLn:
				if p.StartLine < 1 || p.EndLine < p.StartLine {
					return "", fmt.Errorf("start_line >= 1 and end_line >= start_line are required for mode=replace_lines")
				}
				return writeReplaceLinesMode(s, abs, p.StartLine, p.EndLine, p.Content)
			case writeInsertAt:
				if p.StartLine < 1 {
					return "", fmt.Errorf("start_line >= 1 is required for mode=insert_at_line")
				}
				return writeInsertAtMode(s, abs, p.StartLine, p.Content)
			default:
				return "", fmt.Errorf("mode is required: save | replace_string | replace_lines | insert_at_line")
			}
		},
	}
}

// writeSaveMode sets the file's full contents. Creates the file (and any
// missing parent dirs) if absent, replaces it if present. Reports which
// happened in the result string for the agent's benefit, but never errors on
// existence — the agent is declaring intent, not checking state.
func writeSaveMode(s *Session, abs, content string) (string, error) {
	before, statErr := os.ReadFile(abs)
	existed := statErr == nil
	if dir := filepath.Dir(abs); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}
	priorContent := ""
	if existed {
		priorContent = string(before)
	}
	s.RecordEdit(abs, priorContent, content)
	if err := writeBytes(abs, []byte(content)); err != nil {
		return "", err
	}
	if existed {
		return "saved " + abs + " (replaced)", nil
	}
	return "saved " + abs + " (created)", nil
}

func writeReplaceStringMode(s *Session, abs, oldStr, newStr string) (string, error) {
	before, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	old := string(before)
	count := strings.Count(old, oldStr)
	if count == 0 {
		return "", fmt.Errorf("old_str not found — verify exact whitespace, or use mode=replace_lines for deterministic line-based edits")
	}
	if count > 1 {
		return "", fmt.Errorf("old_str matches %d times — provide more surrounding context to make it unique, or use mode=replace_lines", count)
	}
	after := strings.Replace(old, oldStr, newStr, 1)
	s.RecordEdit(abs, old, after)
	return "replaced 1 occurrence in " + abs, writeBytes(abs, []byte(after))
}

func writeReplaceLinesMode(s *Session, abs string, startLine, endLine int, content string) (string, error) {
	before, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	old := string(before)
	lines := strings.Split(old, "\n")
	if startLine > len(lines) {
		return "", fmt.Errorf("start_line %d is past end of file (%d lines)", startLine, len(lines))
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	head := lines[:startLine-1]
	tail := lines[endLine:]
	replacement := strings.Split(strings.TrimRight(content, "\n"), "\n")
	newLines := append(append(append([]string{}, head...), replacement...), tail...)
	after := strings.Join(newLines, "\n")
	s.RecordEdit(abs, old, after)
	return fmt.Sprintf("replaced lines %d-%d in %s", startLine, endLine, abs), writeBytes(abs, []byte(after))
}

func writeInsertAtMode(s *Session, abs string, startLine int, content string) (string, error) {
	before, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	old := string(before)
	lines := strings.Split(old, "\n")
	if startLine > len(lines)+1 {
		return "", fmt.Errorf("start_line %d is past end of file (%d lines)", startLine, len(lines))
	}
	head := lines[:startLine-1]
	tail := lines[startLine-1:]
	insert := strings.Split(strings.TrimRight(content, "\n"), "\n")
	newLines := append(append(append([]string{}, head...), insert...), tail...)
	after := strings.Join(newLines, "\n")
	s.RecordEdit(abs, old, after)
	return fmt.Sprintf("inserted at line %d in %s", startLine, abs), writeBytes(abs, []byte(after))
}
