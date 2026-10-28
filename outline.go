package axon

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Deterministic code map: the directory tree and each file's declarations,
// with line numbers, found by pattern and not by a model. Locating code is
// the bulk of a coding agent's requests (a traced run on 2026-10-24 spent its
// first 60 requests reading directories and paging through files one at a
// time); a map answers "where is X" in one call, for free, in milliseconds.

// skipDirs are never listed or walked: generated, vendored or tool state.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, "out": true, ".gradle": true, ".idea": true, ".vscode": true,
	"__pycache__": true, ".venv": true, "venv": true, ".next": true, ".cache": true,
	"coverage": true, ".dart_tool": true, "Pods": true, ".terraform": true,
}

// declPatterns find a language's declarations, one line each.
var declPatterns = map[string]*regexp.Regexp{
	".go":    regexp.MustCompile(`^(func|type|var|const)\b`),
	".py":    regexp.MustCompile(`^\s*(async\s+def|def|class)\s+\w`),
	".js":    regexp.MustCompile(`^\s*(export\s+)?(default\s+)?(async\s+)?(function\*?|class)\s+\w|^(export\s+)?(const|let|var)\s+\w+\s*=`),
	".ts":    regexp.MustCompile(`^\s*(export\s+)?(default\s+)?(declare\s+)?(abstract\s+)?(async\s+)?(function\*?|class|interface|type|enum|namespace)\s+\w|^(export\s+)?(const|let)\s+\w+\s*[=:]`),
	".kt":    regexp.MustCompile(`^\s*((public|private|protected|internal|open|abstract|override|data|sealed|suspend|inline|enum|annotation|inner|value|operator|tailrec|lateinit|const)\s+)*(fun|class|interface|object|typealias)\b`),
	".java":  regexp.MustCompile(`^\s*((public|private|protected|static|final|abstract|sealed|synchronized|default)\s+)*(class|interface|enum|record|@interface)\s+\w|^\s+((public|private|protected|static|final|abstract|synchronized)\s+)+[\w<>\[\], ?]+\s+\w+\s*\(`),
	".rs":    regexp.MustCompile(`^\s*(pub(\([\w:]+\))?\s+)?(async\s+)?(unsafe\s+)?(fn|struct|enum|trait|impl|mod|type|macro_rules!)\b`),
	".rb":    regexp.MustCompile(`^\s*(def|class|module)\s+\w`),
	".swift": regexp.MustCompile(`^\s*((public|private|internal|fileprivate|open|static|final|override|mutating)\s+)*(func|class|struct|enum|protocol|extension|actor)\s+\w`),
	".cs":    regexp.MustCompile(`^\s*((public|private|protected|internal|static|sealed|abstract|partial|async|override|virtual)\s+)+(class|interface|struct|enum|record|[\w<>\[\],]+\s+\w+\s*\()`),
	".php":   regexp.MustCompile(`^\s*((public|private|protected|static|abstract|final)\s+)*(function|class|interface|trait|enum)\s+\w`),
	".sh":    regexp.MustCompile(`^\s*(function\s+\w+|\w+\s*\(\)\s*\{)`),
	".md":    regexp.MustCompile(`^#{1,3}\s`),
}

func declPattern(path string) *regexp.Regexp {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".tsx", ".mts", ".cts":
		ext = ".ts"
	case ".jsx", ".mjs", ".cjs":
		ext = ".js"
	case ".kts":
		ext = ".kt"
	case ".bash":
		ext = ".sh"
	case ".markdown":
		ext = ".md"
	}
	return declPatterns[ext]
}

const (
	outlineFileLines = 60  // declarations shown per file
	outlineTotal     = 600 // lines one outline call returns
	outlineFiles     = 400 // files one directory outline visits
	treeEntries      = 300 // entries a directory listing shows
)

// fileOutline returns path's declarations as "line: text", or nil when the
// language has no pattern, and how many more there were past max.
func fileOutline(path string, max int) ([]string, int) {
	re := declPattern(path)
	if re == nil {
		return nil, 0
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	out := []string{}
	more, n := 0, 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if !re.MatchString(line) {
			continue
		}
		if len(out) >= max {
			more++
			continue
		}
		// Indentation stays: it is what tells a method from a function.
		text := strings.TrimRight(line, " \t{")
		if r := []rune(text); len(r) > 160 {
			text = string(r[:160]) + "…"
		}
		out = append(out, fmt.Sprintf("%d: %s", n, text))
	}
	return out, more
}

// outline renders the declarations of a file, or of every source file under a
// directory.
func outline(abs string) (string, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		decls, more := fileOutline(abs, outlineTotal)
		if decls == nil {
			return fmt.Sprintf("[no outline for %s: unknown language; read it, or search it]", abs), nil
		}
		out := fmt.Sprintf("[outline: %s — %d declarations]\n%s", abs, len(decls)+more, strings.Join(decls, "\n"))
		if more > 0 {
			out += fmt.Sprintf("\n[%d more]", more)
		}
		return out, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[outline: %s — declarations by file, \"line: text\"]\n", abs)
	total, files, cut := 0, 0, false
	_ = filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != abs && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if declPattern(p) == nil {
			return nil
		}
		if files++; files > outlineFiles || total >= outlineTotal {
			cut = true
			return filepath.SkipAll
		}
		decls, more := fileOutline(p, outlineFileLines)
		rel, _ := filepath.Rel(abs, p)
		fmt.Fprintf(&b, "\n%s\n", filepath.ToSlash(rel))
		for _, d := range decls {
			b.WriteString("  " + d + "\n")
		}
		if more > 0 {
			fmt.Fprintf(&b, "  [%d more]\n", more)
		}
		total += len(decls) + 1
		return nil
	})
	if cut {
		b.WriteString("\n[outline cut short; outline a subdirectory for the rest]")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// dirTree lists a directory two levels deep, generated and tool directories
// left out, so one call shows a project's layout instead of one call per
// directory.
func dirTree(abs string) (string, error) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "[directory: " + abs + " — empty]", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[directory: %s — %d entries, shown two levels deep; trailing '/' = subdir; generated and tool dirs left out]\n", abs, len(entries))
	shown := 0
	var walk func(dir, indent string, depth int)
	walk = func(dir, indent string, depth int) {
		list, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].IsDir() && !list[j].IsDir() })
		for i, e := range list {
			if shown >= treeEntries {
				fmt.Fprintf(&b, "%s[%d more entries]\n", indent, len(list)-i)
				return
			}
			name := e.Name()
			if !e.IsDir() {
				shown++
				fmt.Fprintf(&b, "%s%s\n", indent, name)
				continue
			}
			if skipDirs[name] || (depth > 0 && strings.HasPrefix(name, ".")) {
				continue
			}
			shown++
			sub, _ := os.ReadDir(filepath.Join(dir, name))
			if depth > 0 {
				fmt.Fprintf(&b, "%s%s/ (%d entries)\n", indent, name, len(sub))
				continue
			}
			fmt.Fprintf(&b, "%s%s/\n", indent, name)
			if len(sub) > 0 {
				walk(filepath.Join(dir, name), indent+"  ", depth+1)
			}
		}
	}
	walk(abs, "", 0)
	return strings.TrimRight(b.String(), "\n"), nil
}
