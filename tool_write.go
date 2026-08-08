package axon

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

type writeInput struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	Content   string `json:"content"`
	OldStr    string `json:"old_str"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func parseAndValidateWriteInput(raw json.RawMessage, ws Workspace) (*writeInput, string, error) {
	var p writeInput
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(p.Path) == "" {
		return nil, "", fmt.Errorf("path is required")
	}
	abs := ws.ResolvePath(p.Path)

	switch p.Mode {
	case writeSave:
		// no extra validation needed
	case writeReplaceStr:
		if p.OldStr == "" {
			return nil, "", fmt.Errorf("old_str is required for mode=replace_string (use overwrite if you mean to replace the whole file)")
		}
	case writeReplaceLn:
		if p.StartLine < 1 || p.EndLine < p.StartLine {
			return nil, "", fmt.Errorf("start_line >= 1 and end_line >= start_line are required for mode=replace_lines")
		}
	case writeInsertAt:
		if p.StartLine < 1 {
			return nil, "", fmt.Errorf("start_line >= 1 is required for mode=insert_at_line")
		}
	default:
		return nil, "", fmt.Errorf("mode is required: save | replace_string | replace_lines | insert_at_line")
	}
	return &p, abs, nil
}

func WriteTool(ws Workspace) Tool {
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
		}, []string{"path", "mode", "content"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			p, abs, err := parseAndValidateWriteInput(raw, ws)
			if err != nil {
				return "", err
			}

			switch p.Mode {
			case writeSave:
				return writeSaveMode(ws, abs, p.Content)
			case writeReplaceStr:
				return writeReplaceStringMode(ws, abs, p.OldStr, p.Content)
			case writeReplaceLn:
				return writeReplaceLinesMode(ws, abs, p.StartLine, p.EndLine, p.Content)
			case writeInsertAt:
				return writeInsertAtMode(ws, abs, p.StartLine, p.Content)
			default:
				return "", fmt.Errorf("unhandled mode")
			}
		},
	}
}

// writeSaveMode sets the file's full contents. Creates the file (and any
// missing parent dirs) if absent, replaces it if present. Reports which
// happened in the result string for the agent's benefit, but never errors on
// existence — the agent is declaring intent, not checking state.
func writeSaveMode(ws Workspace, abs, content string) (string, error) {
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
	ws.RecordEdit(abs, priorContent)
	if err := WriteFileAtomic(abs, []byte(content)); err != nil {
		return "", err
	}
	if existed {
		return "saved " + abs + " (replaced)", nil
	}
	return "saved " + abs + " (created)", nil
}

func writeReplaceStringMode(ws Workspace, abs, oldStr, newStr string) (string, error) {
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
	ws.RecordEdit(abs, old)
	return "replaced 1 occurrence in " + abs, WriteFileAtomic(abs, []byte(after))
}

func writeReplaceLinesMode(ws Workspace, abs string, startLine, endLine int, content string) (string, error) {
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
	ws.RecordEdit(abs, old)
	return fmt.Sprintf("replaced lines %d-%d in %s", startLine, endLine, abs), WriteFileAtomic(abs, []byte(after))
}

func writeInsertAtMode(ws Workspace, abs string, startLine int, content string) (string, error) {
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
	ws.RecordEdit(abs, old)
	return fmt.Sprintf("inserted at line %d in %s", startLine, abs), WriteFileAtomic(abs, []byte(after))
}
