package axon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// ---------------------------------------------------------------------------
// READ
// ---------------------------------------------------------------------------

const readDescription = `Read files, list a directory, or map code.
  - slice: lines [offset, offset+limit). The default; limit defaults to enough for most whole files.
  - full: entire file.
  - outline: declarations with line numbers ("line: text"), of a file or of every source file under a directory. The fastest way to find where things are: outline first, then read the ranges you need.
If path is a directory (and mode is not outline), returns its tree two levels deep.
To read several files or ranges at once, pass them all in "files" instead of
"path": one call, one round trip. Decide everything you need first, then fetch
it together.
Reading text that is already in your context and unchanged returns a pointer
to the earlier block instead of a second copy.`

// maxReadBatch bounds one batched read so a single call cannot flood context.
const maxReadBatch = 20

type readInput struct {
	Path   string      `json:"path"`
	Mode   string      `json:"mode"`
	Offset int         `json:"offset"`
	Limit  int         `json:"limit"`
	Files  []readInput `json:"files"`
}

func parseAndValidateReadInput(raw json.RawMessage, ws Workspace) (*readInput, string, error) {
	var p readInput
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "", err
	}
	if len(p.Files) > 0 {
		if strings.TrimSpace(p.Path) != "" {
			return nil, "", fmt.Errorf("pass either path or files, not both")
		}
		if len(p.Files) > maxReadBatch {
			return nil, "", fmt.Errorf("at most %d files per read", maxReadBatch)
		}
		return &p, "", nil
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

func ReadTool(ws Workspace, lim Limits) Tool {
	limit := lim.ReadLines
	one := func(p readInput, abs string) (string, error) {
		if p.Mode == readOutline {
			return outline(abs)
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return dirTree(abs)
		}
		if msg, binary := binaryFileRefusal(abs); binary {
			return msg, nil
		}
		switch p.Mode {
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
			return "", fmt.Errorf("unknown mode %q: slice | full | outline", p.Mode)
		}
	}
	return Tool{
		Name:        toolRead,
		Description: readDescription,
		Schema: obj("object", props{
			"path":   strSchema("Relative or absolute file path. Omit when using files."),
			"mode":   enumSchema("slice | full | outline. Optional; defaults to slice.", readSlice, readFull, readOutline),
			"offset": intSchema("1-based start line. Used when mode=slice."),
			"limit":  intSchema(fmt.Sprintf("Lines to return. Used when mode=slice. Default %d if omitted.", limit)),
			"files": arr(obj("object", props{
				"path":   strSchema("Relative or absolute file path."),
				"mode":   enumSchema("slice | full | outline. Optional; defaults to slice.", readSlice, readFull, readOutline),
				"offset": intSchema("1-based start line. Used when mode=slice."),
				"limit":  intSchema("Lines to return. Used when mode=slice."),
			}, []string{"path"})),
		}, nil),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			p, abs, err := parseAndValidateReadInput(raw, ws)
			if err != nil {
				return "", err
			}
			if len(p.Files) == 0 {
				return one(*p, abs)
			}
			// One failed entry is reported in place; it must not discard the
			// files that did read.
			var b strings.Builder
			for i, f := range p.Files {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				if i > 0 {
					b.WriteString("\n\n")
				}
				label := f.Path
				if f.Mode != readFull && f.Offset > 0 {
					label += fmt.Sprintf(" (from line %d)", f.Offset)
				}
				fmt.Fprintf(&b, "=== %s ===\n", label)
				if strings.TrimSpace(f.Path) == "" {
					b.WriteString("[error: path is required]")
					continue
				}
				if f.Mode == "" {
					f.Mode = readSlice
				}
				out, err := one(f, ws.ResolvePath(f.Path))
				if err != nil {
					out = "[error: " + err.Error() + "]"
				}
				b.WriteString(out)
			}
			return b.String(), nil
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
	header := fmt.Sprintf("[full read: ~%d tokens. use mode=slice if you need only part of this file]\n", approx)
	return header + strings.TrimRight(b.String(), "\n"), nil
}
