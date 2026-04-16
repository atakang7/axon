package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const defaultReadLimit = 200

var ReadFileDefinition = ToolDefinition{
	Name:        "read_file",
	Description: fmt.Sprintf("Read lines from a file. Do not assume file paths or directory structure — use list_files first. Returns at most %d lines. Use offset+limit to page through larger files.", defaultReadLimit),
	InputSchema: GenerateSchema[ReadFileInput](),
	Function:    ReadFile,
}

type ReadFileInput struct {
	Path   string `json:"path" jsonschema_description:"Relative path to the file."`
	Offset int    `json:"offset,omitempty" jsonschema_description:"Line number to start from (1-based). Defaults to 1."`
	Limit  int    `json:"limit,omitempty" jsonschema_description:"Max lines to return. Defaults to 200."`
}

func ReadFile(input json.RawMessage) (string, error) {
	var p ReadFileInput
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	f, err := os.Open(p.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if p.Offset < 1 {
		p.Offset = 1
	}
	if p.Limit <= 0 {
		p.Limit = defaultReadLimit
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		if n < p.Offset {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d\t%s", n, scanner.Text()))
		if len(lines) >= p.Limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}
