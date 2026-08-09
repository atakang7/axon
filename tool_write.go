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
// WRITE
// ---------------------------------------------------------------------------

// writeDescription is the model's only guidance on how to change a file, so
// the mode ordering here is the mode ranking the model adopts.
//
// It used to lead with save and told the model to use it "whenever you have
// the whole file in hand". Since a file is almost always changed right after
// being read, that condition is nearly always true, and traced runs showed
// the consequence: five consecutive full-file rewrites for a change touching
// a few lines each, re-emitting every unchanged line as output tokens. The
// targeted modes were never chosen once.
//
// Ordering now runs narrowest-first and names the condition for save in terms
// of the edit rather than the model's context.
const writeDescription = `Write to a file. Choose the mode by the size of the change, not by how much of the file you happen to have in hand.
  - replace_string: replace one exact occurrence of old_str. This is the normal way to change a file that already exists. Include enough surrounding context in old_str for the match to be unique.
  - replace_lines: replace lines [start_line, end_line]. Use when the text is long or awkward to quote exactly. Take the numbers from a fresh read of the file, not from memory, and include every line you intend to replace — a range that stops one line short silently deletes whatever closed the block.
  - insert_at_line: insert before start_line (1-based). Use to add without disturbing what is already there.
  - save: set the file's full contents. Correct for a new file, or when genuinely replacing nearly all of an existing one.

Having just read a file is not a reason to rewrite it. A full rewrite costs output in proportion to the file's size instead of the change's, and it silently drops anything you did not carry over.

Writes are atomic (tmp + rename) and each one is recorded so it can be reverted. A formatter runs after every write. For brace languages emit content flat (no indentation). For whitespace-significant languages (Python, YAML) emit indentation correctly.`

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
			// The mode named here must be one that exists. It read
			// "use overwrite" for a while — there is no overwrite mode, and
			// a caller following the advice gets a second, equally opaque
			// error out of the default branch below.
			return nil, "", fmt.Errorf("old_str is required for mode=replace_string (use mode=save if you mean to replace the whole file)")
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
			"path": strSchema("Relative or absolute file path."),
			// Order matters here, and it is the reason this line reads oddly
			// against the constant block above. The enum led with save, and
			// so did its description; traced runs showed the model picking
			// save every single time, even after the prose in
			// writeDescription was rewritten to argue against it. The
			// structured schema is the stronger signal, so the narrow modes
			// go first and save goes last, with the condition for each
			// stated where the model reads the field.
			"mode": enumSchema(
				"replace_string | replace_lines | insert_at_line | save. Required. Use replace_string for an ordinary change to a file that already exists; use save only for a new file or a near-total rewrite.",
				writeReplaceStr, writeReplaceLn, writeInsertAt, writeSave,
			),
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

	// Caught before the match is attempted, because it explains a failure the
	// not-found message below would describe misleadingly. A caller that sends
	// the same text as both halves has written out the state it wants the file
	// to be in and used it for both fields; old_str then describes a file that
	// does not exist yet, so the match fails for a reason that has nothing to
	// do with whitespace.
	if oldStr == newStr {
		return "", fmt.Errorf("old_str and content are identical, so this edit would change nothing — old_str must be the text as it appears in the file now, content the text that should replace it")
	}

	count := strings.Count(old, oldStr)
	if count == 0 {
		// The recovery advice here used to point at replace_lines. That is
		// the wrong direction and it caused real damage: old_str not matching
		// means the caller's picture of this file is stale, and line numbers
		// derived from that same stale picture are stale too. A traced run
		// took the advice, replaced a line range computed from memory, and
		// destroyed a docstring's closing quotes — then spent two more calls
		// repairing it. Send the caller back to the file instead.
		return "", fmt.Errorf("old_str not found in %s — read the file again and copy the exact text you mean to replace, including its whitespace, rather than reconstructing it from memory", abs)
	}
	if count > 1 {
		return "", fmt.Errorf("old_str matches %d times in %s — extend it with the surrounding lines that make the one you mean unique", count, abs)
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
