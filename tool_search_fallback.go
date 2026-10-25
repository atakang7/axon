package axon

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Built-in search, used when rg is not installed. Without it the search tool
// failed outright on such machines, for the agent and the explorer alike —
// seen live on 2026-10-26, where the explorer spent its whole budget asking
// for a shell to find rg. It is slower than rg and does not read .gitignore,
// so it skips the usual generated and vendored directories instead, and says
// so in its output.

// fallbackSkipDirs are never searched by the built-in search.
var fallbackSkipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, "target": true, "out": true, ".next": true,
	".venv": true, "venv": true, "__pycache__": true, ".idea": true, ".gradle": true,
}

const (
	fallbackMaxFiles    = 50000
	fallbackMaxFileSize = 2 << 20
	fallbackLineCap     = 500
)

func searchFallback(parent context.Context, ws Workspace, lim Limits, p searchInput, literal bool) (string, error) {
	ctx, cancel := context.WithTimeout(parent, lim.SearchTimeout)
	defer cancel()

	match, err := lineMatcher(p.Query, literal, p.CaseSensitive)
	if err != nil {
		return "", err
	}
	include, exclude := splitGlobs(p.Globs)
	root := ws.ResolvePath(p.Path)
	base := ws.Dir()
	if base == "" {
		base = root
	}

	var out strings.Builder
	truncated := false
	files := 0
	write := func(line string) bool {
		if lim.SearchOutputBytes > 0 && out.Len()+len(line)+1 > lim.SearchOutputBytes {
			truncated = true
			return false
		}
		out.WriteString(line)
		out.WriteByte('\n')
		return true
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, like rg does
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != root && fallbackSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if !globsAllow(rel, include, exclude) {
			return nil
		}
		files++
		if files > fallbackMaxFiles {
			truncated = true
			return fs.SkipAll
		}
		if info, err := d.Info(); err != nil || info.Size() > fallbackMaxFileSize {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(data[:min(len(data), 8192)], 0) >= 0 {
			return nil
		}
		hits := 0
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 64*1024), fallbackMaxFileSize)
		for n := 1; sc.Scan(); n++ {
			text := strings.TrimRight(sc.Text(), "\r")
			if !match(text) {
				continue
			}
			if len(text) > fallbackLineCap {
				text = text[:fallbackLineCap] + "…"
			}
			if !write(fmt.Sprintf("%s:%d:%s", rel, n, text)) {
				return fs.SkipAll
			}
			hits++
			if p.MaxMatches > 0 && hits >= p.MaxMatches {
				break
			}
		}
		return nil
	})
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return "", fmt.Errorf("search timed out after %s", lim.SearchTimeout)
	case parent.Err() != nil:
		return "", parent.Err()
	case walkErr != nil && walkErr != fs.SkipAll:
		return "", walkErr
	}

	var b strings.Builder
	fmt.Fprintf(&b, "query: %s\npath: %s\n[built-in search: rg is not installed, so .gitignore is not applied; generated and vendored directories are skipped]\n", p.Query, p.Path)
	if out.Len() == 0 {
		b.WriteString("no matches")
	} else {
		b.WriteString(strings.TrimRight(out.String(), "\n"))
	}
	if truncated {
		b.WriteString("\n[output truncated]")
	}
	return b.String(), nil
}

// lineMatcher returns a predicate for one line: a substring for literal
// queries, a regular expression otherwise; case-insensitive unless asked.
func lineMatcher(query string, literal, caseSensitive bool) (func(string) bool, error) {
	if literal {
		if caseSensitive {
			return func(s string) bool { return strings.Contains(s, query) }, nil
		}
		q := strings.ToLower(query)
		return func(s string) bool { return strings.Contains(strings.ToLower(s), q) }, nil
	}
	pattern := query
	if !caseSensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %v", err)
	}
	return re.MatchString, nil
}

// splitGlobs separates rg-style globs into includes and "!" excludes.
func splitGlobs(globs []string) (include, exclude []string) {
	for _, g := range globs {
		g = strings.TrimSpace(g)
		switch {
		case g == "":
		case strings.HasPrefix(g, "!"):
			exclude = append(exclude, strings.TrimPrefix(g, "!"))
		default:
			include = append(include, g)
		}
	}
	return include, exclude
}

// globsAllow applies globs the way rg does for the common cases: a pattern
// without a slash matches the file name, one with a slash matches the path,
// and a leading "**/" matches at any depth.
func globsAllow(rel string, include, exclude []string) bool {
	matches := func(g string) bool {
		g = strings.TrimPrefix(g, "**/")
		if strings.Contains(g, "/") {
			if ok, _ := filepath.Match(g, rel); ok {
				return true
			}
			return strings.HasPrefix(rel, strings.TrimSuffix(strings.TrimSuffix(g, "**"), "/")+"/") && strings.HasSuffix(g, "**")
		}
		ok, _ := filepath.Match(g, filepath.Base(rel))
		return ok
	}
	for _, g := range exclude {
		if matches(g) {
			return false
		}
	}
	if len(include) == 0 {
		return true
	}
	for _, g := range include {
		if matches(g) {
			return true
		}
	}
	return false
}
