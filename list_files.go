package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var ListFilesDefinition = ToolDefinition{
	Name:        "list_files",
	Description: "List files and directories at a given path. Always call this before read_file — never assume file paths. Large files (>1MB) are flagged — do not read them fully.",
	InputSchema: GenerateSchema[ListFilesInput](),
	Function:    ListFiles,
}

type ListFilesInput struct {
	Path string `json:"path,omitempty" jsonschema_description:"Optional relative path. Defaults to current directory."`
}

func ListFiles(input json.RawMessage) (string, error) {
	var p ListFilesInput
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	dir := "."
	if p.Path != "" {
		dir = p.Path
	}

	var entries []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		if info.IsDir() {
			entries = append(entries, rel+"/")
		} else if info.Size() > 1<<20 {
			entries = append(entries, fmt.Sprintf("%s [large: %dMB, do not read fully]", rel, info.Size()>>20))
		} else {
			entries = append(entries, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
