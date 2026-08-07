package agent

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The layering rule documented in ARCHITECTURE.md, enforced as a test so the
// documentation cannot quietly drift from the code.
//
//	axon  <-  config  <-  llm  <-  session  <-  tools  <-  agent
//
// A package may import anything at a strictly lower depth and nothing at the
// same or higher depth.
//
// While the chain is total — each layer importing the one below — Go's own
// cycle detection already rejects any upward import, and it is the primary
// guard. This test covers what that does not:
//
//   - A new package added outside the documented layering. Go is happy; the
//     architecture doc silently stops describing the code.
//   - A wrong-direction import that is not a cycle. If some layer ever stops
//     importing the one below it (say session no longer needs llm), then
//     llm -> session compiles cleanly while inverting the design.
//
// Both are the failure mode where documentation drifts from reality, which is
// exactly what this file exists to prevent.
var depth = map[string]int{
	"axon":    0, // the public vocabulary; a leaf, imports nothing
	"config":  1,
	"llm":     2,
	"session": 3,
	"tools":   4,
	"agent":   5,
}

const modulePath = "github.com/atakang7/axon"

func TestLayeringIsAcyclicAndOneDirectional(t *testing.T) {
	root := ".." // tests run in the package directory

	for pkg := range depth {
		dir := filepath.Join(root, "internal", pkg)
		switch pkg {
		case "agent":
			dir = filepath.Join(root, "agent")
		case "axon":
			dir = root // the root package lives at the repository root
		}
		for _, imported := range intraModuleImports(t, dir) {
			from, to := depth[pkg], depth[imported]
			if _, ok := depth[imported]; !ok {
				t.Errorf("%s imports %s, which is not part of the documented layering", pkg, imported)
				continue
			}
			if from <= to {
				t.Errorf("layering violation: %s (depth %d) imports %s (depth %d); a package may import only strictly lower layers",
					pkg, from, imported, to)
			}
		}
	}
}

// The two bottom layers must stay free of intra-module dependencies. If a
// value there ever needs a type from a higher layer, the value belongs in that
// layer instead.
func TestLeavesImportNothing(t *testing.T) {
	for _, dir := range []string{"..", filepath.Join("..", "internal", "config")} {
		if got := intraModuleImports(t, dir); len(got) > 0 {
			t.Fatalf("%s must import nothing from this module, got %v", dir, got)
		}
	}
}

// The internal packages must stay unreachable from outside the module. If one
// ever moves out from under internal/, an embedder can depend on it and it
// stops being free to change.
func TestInternalsStayInternal(t *testing.T) {
	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "internal" {
			continue
		}
		for pkg := range depth {
			if pkg == "agent" {
				continue
			}
			if e.Name() == pkg {
				t.Errorf("%s must live under internal/, found it at repository root", pkg)
			}
		}
	}
}

// intraModuleImports returns the short names of packages in this module that
// dir's non-test files import.
func intraModuleImports(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}

	seen := map[string]bool{}
	var out []string
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, imp := range f.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil || !strings.HasPrefix(path, modulePath) {
					continue
				}
				name := path[strings.LastIndexByte(path, '/')+1:]
				if !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
			}
		}
	}
	return out
}
