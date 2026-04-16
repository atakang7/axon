package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func EditFileDefinition(s *Session) ToolDefinition {
	return ToolDefinition{
		Name: "edit_file",
		Description: `Edit a file by replacing old_str with new_str. They must differ.
If the file does not exist and old_str is empty, it will be created.
old_str must match exactly once.`,
		InputSchema: GenerateSchema[EditFileInput](),
		Function:    editFile(s),
	}
}

type EditFileInput struct {
	Path   string `json:"path" jsonschema_description:"Path to the file"`
	OldStr string `json:"old_str" jsonschema_description:"Text to replace — must match exactly once"`
	NewStr string `json:"new_str" jsonschema_description:"Replacement text"`
}

func editFile(s *Session) func(json.RawMessage) (string, error) {
	return func(input json.RawMessage) (string, error) {
		var p EditFileInput
		if err := json.Unmarshal(input, &p); err != nil {
			return "", err
		}
		if p.Path == "" || p.OldStr == p.NewStr {
			return "", fmt.Errorf("invalid input")
		}
		before, err := os.ReadFile(p.Path)
		if err != nil {
			if os.IsNotExist(err) && p.OldStr == "" {
				if dir := filepath.Dir(p.Path); dir != "." {
					os.MkdirAll(dir, 0755)
				}
				s.RecordEdit(p.Path, "", p.NewStr)
				return "created", writeFileBytes(p.Path, []byte(p.NewStr))
			}
			return "", err
		}
		old := string(before)
		if count := strings.Count(old, p.OldStr); count == 0 {
			return "", fmt.Errorf("old_str not found")
		} else if count > 1 {
			return "", fmt.Errorf("old_str matches %d times — be more specific", count)
		}
		after := strings.Replace(old, p.OldStr, p.NewStr, 1)
		s.RecordEdit(p.Path, old, after)
		return "OK", writeFileBytes(p.Path, []byte(after))
	}
}

func writeFileBytes(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
