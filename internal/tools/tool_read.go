package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/atakang7/axon/internal/config"
)

// ---------------------------------------------------------------------------
// READ — single-file content. Three modes, conscious pick.
// ---------------------------------------------------------------------------

const readDescription = `Read one file, or list a directory.
  - skeleton: signatures only. ~10x cheaper than full.
  - slice: lines [offset, offset+limit).
  - full: entire file.
If path is a directory, returns its entries (subdirs marked with trailing '/'); mode is ignored.`

type readInput struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func parseAndValidateReadInput(raw json.RawMessage, ws Workspace) (*readInput, string, error) {
	var p readInput
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(p.Path) == "" {
		return nil, "", fmt.Errorf("path is required")
	}
	if p.Mode == "" {
		p.Mode = readSlice
	}
	abs := ws.ResolvePath(p.Path)
	return &p, abs, nil
}

func ReadTool(ws Workspace, lim config.Limits) Tool {
	limit := lim.ReadLines
	return Tool{
		Name:        toolRead,
		Description: readDescription,
		Schema: obj("object", props{
			"path":   strSchema("Relative or absolute file path."),
			"mode":   enumSchema("skeleton | slice | full. Optional; defaults to slice.", readSkeleton, readSlice, readFull),
			"offset": intSchema("1-based start line. Used when mode=slice."),
			"limit":  intSchema(fmt.Sprintf("Lines to return. Used when mode=slice. Default %d if omitted.", limit)),
		}, []string{"path"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			p, abs, err := parseAndValidateReadInput(raw, ws)
			if err != nil {
				return "", err
			}
			if info, err := os.Stat(abs); err == nil && info.IsDir() {
				return readDirListing(abs)
			}
			if msg, binary := binaryFileRefusal(abs); binary {
				return msg, nil
			}
			switch p.Mode {
			case readSkeleton:
				return readSkeletonMode(abs)
			case readSlice:
				if p.Offset < 1 {
					p.Offset = 1
				}
				if p.Limit <= 0 {
					p.Limit = limit
				}
				return readSliceMode(abs, p.Offset, p.Limit)
			case readFull:
				return readFullMode(abs, lim.ReadMaxBytes)
			default:
				return "", fmt.Errorf("unknown mode %q: skeleton | slice | full", p.Mode)
			}
		},
	}
}

func readSliceMode(abs string, offset, limit int) (string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()

	const lineDisplayCap = 8192
	r := bufio.NewReader(f)
	var out []string
	line := 0
	for {
		line++
		text, err := r.ReadString('\n')
		eof := err != nil
		if err != nil && err != io.EOF {
			return "", err
		}
		if line >= offset {
			text = strings.TrimRight(text, "\n")
			if len(text) > lineDisplayCap {
				text = text[:lineDisplayCap] + "...[line truncated]"
			}
			out = append(out, fmt.Sprintf("%d\t%s", line, text))
			if len(out) >= limit {
				break
			}
		}
		if eof {
			break
		}
	}
	if len(out) == 0 {
		return "[empty range]", nil
	}
	return strings.Join(out, "\n"), nil
}

func readFullMode(abs string, maxBytes int) (string, error) {
	fi, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	max := int64(maxBytes)
	if fi.Size() > max {
		return fmt.Sprintf("[full read refused: %s is %d bytes (>%d cap). Use mode=slice to page through, or raise AXON_READ_MAX_BYTES.]", abs, fi.Size(), max), nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%d\t%s\n", i+1, line)
	}
	approx := len(data) / 4
	header := fmt.Sprintf("[full read: ~%d tokens. consider mode=skeleton (~10x cheaper) or mode=slice if you need only part of this file]\n", approx)
	return header + strings.TrimRight(b.String(), "\n"), nil
}

var skeletonPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*(package|import|from|using|namespace)\b`),
	regexp.MustCompile(`^\s*(func|function|def|class|type|struct|interface|trait|enum|impl|module)\b`),
	regexp.MustCompile(`^\s*(public|private|protected|static|export|async|const|let|var)\s+(function|class|def|fn|struct|type|interface|enum)\b`),
	regexp.MustCompile(`^\s*(export\s+)?(default\s+)?(function|class|const|let|var|async)\b.*[({]`),
}

func readDirListing(abs string) (string, error) {
	fis, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	if len(fis) == 0 {
		return "[directory: " + abs + " — empty]", nil
	}
	var dirs, files []string
	for _, fi := range fis {
		if fi.IsDir() {
			dirs = append(dirs, fi.Name()+"/")
		} else {
			files = append(files, fi.Name())
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[directory: %s — %d entries. trailing '/' = subdir. Read a specific entry next.]\n", abs, len(fis))
	for _, d := range dirs {
		b.WriteString(d + "\n")
	}
	for _, f := range files {
		b.WriteString(f + "\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func readSkeletonMode(abs string) (string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()

	const skelLineCap = 4096
	r := bufio.NewReader(f)
	var out []string
	line := 0
	for {
		line++
		text, err := r.ReadString('\n')
		eof := err != nil
		if err != nil && err != io.EOF {
			return "", err
		}
		text = strings.TrimRight(text, "\n")
		if len(text) <= skelLineCap {
			for _, re := range skeletonPatterns {
				if re.MatchString(text) {
					out = append(out, fmt.Sprintf("%d\t%s", line, text))
					break
				}
			}
		}
		if eof {
			break
		}
	}
	if len(out) == 0 {
		return "[skeleton found no signatures]", nil
	}
	return strings.Join(out, "\n"), nil
}
