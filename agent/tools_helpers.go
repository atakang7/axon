package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// tools_helpers.go — shared helpers used by every tool: atomic writes,
// formatter dispatch, binary-file refusal, output capping, reason check.

// ---------------------------------------------------------------------------
// Shared utilities
// ---------------------------------------------------------------------------

// writeBytesRaw writes atomically: tmp file in the same dir, then rename. The
// rename is atomic on POSIX, so a crash mid-write never leaves a half-written
// file at `path` — readers see either the old contents or the new, never a
// truncated mix. Same-dir tmp matters: cross-filesystem renames degrade to
// copy+unlink and lose the atomicity guarantee.
//
// File mode handling: if the destination already exists, its mode is preserved
// (executable bits, group-readable scripts, etc.). New files default to 0644.
// No formatter runs here — callers that want formatting use writeBytes.
func writeBytesRaw(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".axon-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// writeBytes writes atomically and then runs the language-appropriate
// formatter best-effort. Use this from edit tools. Use writeBytesRaw from
// /undo and any other path that must be byte-exact.
func writeBytes(path string, data []byte) error {
	if err := writeBytesRaw(path, data); err != nil {
		return err
	}
	formatPath(path)
	return nil
}

// formatPath runs the appropriate formatter for the file's extension.
// Best-effort: any error (missing tool, parse failure, timeout) is swallowed —
// formatting must never break a successful write. The model is allowed to emit
// non-indented code for whitespace-insensitive languages and rely on this hook.
func formatPath(path string) {
	ext := strings.ToLower(filepath.Ext(path))
	cmd, args, ok := formatterFor(ext, path)
	if !ok {
		return
	}
	resolved, err := resolveFormatter(cmd)
	if err != nil {
		return
	}
	cmd = resolved
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, cmd, args...)
	// dprint resolves file paths against cwd, so run it from the file's
	// directory and pass the basename. gofmt etc. don't care.
	if strings.HasSuffix(cmd, "dprint") {
		c.Dir = filepath.Dir(path)
		// Replace the final arg (the absolute path) with just the basename.
		if n := len(args); n > 0 {
			args[n-1] = filepath.Base(path)
			c.Args = append([]string{cmd}, args...)
		}
	}
	_ = c.Run()
}

// resolveFormatter looks up a formatter binary on PATH, falling back to known
// install locations (e.g. dprint's default ~/.dprint/bin) so agents work even
// when the user hasn't shell-rc'd the install path.
func resolveFormatter(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, p := range []string{
		filepath.Join(home, ".dprint", "bin", name),
		filepath.Join(home, ".cargo", "bin", name),
		filepath.Join(home, ".local", "bin", name),
	} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("formatter %q not found", name)
}

// formatterFor maps a file extension to (binary, args). Returns ok=false when
// no formatter is configured for the extension.
//
// dprint covers: TS/JS/JSX/TSX, JSON, MD, TOML, YAML, Python (via ruff plugin),
// CSS, HTML, Dockerfile. Native binaries handle the rest. Order matters:
// native formatters take priority where they're idiomatic (gofmt for Go,
// rustfmt for Rust) so the result matches what the project's CI expects.
func formatterFor(ext, path string) (string, []string, bool) {
	switch ext {
	case ".go":
		return "gofmt", []string{"-w", path}, true
	case ".rs":
		return "rustfmt", []string{path}, true
	case ".sh", ".bash":
		return "shfmt", []string{"-w", path}, true
	case ".c", ".h", ".cpp", ".hpp", ".cc", ".cxx":
		return "clang-format", []string{"-i", path}, true
	case ".java":
		return "google-java-format", []string{"-i", path}, true
	case ".zig":
		return "zig", []string{"fmt", path}, true
	case ".kt", ".kts":
		return "ktlint", []string{"-F", path}, true
	case ".swift":
		return "swift-format", []string{"-i", path}, true
	case ".rb":
		return "rubocop", []string{"-a", "--no-color", path}, true
	case ".php":
		return "php-cs-fixer", []string{"fix", path}, true
	case ".scala", ".sbt":
		return "scalafmt", []string{path}, true
	case ".lua":
		return "stylua", []string{path}, true
	case ".dart":
		return "dart", []string{"format", path}, true
	case ".ex", ".exs":
		return "mix", []string{"format", path}, true
	case ".nix":
		return "nixpkgs-fmt", []string{path}, true
	case ".proto":
		return "buf", []string{"format", "-w", path}, true
	case ".sql":
		return "sqlfluff", []string{"fix", "--disable-progress-bar", path}, true
	case ".tf", ".tfvars":
		return "terraform", []string{"fmt", path}, true
	case ".xml":
		return "xmllint", []string{"--format", "--output", path, path}, true
	case ".py", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs",
		".json", ".jsonc", ".md", ".markdown", ".toml",
		".yaml", ".yml", ".css", ".scss", ".html", ".htm":
		return "dprint", []string{"fmt", "--config", dprintConfigPath(), path}, true
	}
	return "", nil, false
}

// dprintConfigPath returns the path to the agent's dprint config, creating it
// on first use. We keep one config under the user's config dir so dprint has
// a stable place to find plugins regardless of cwd.
func dprintConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "dprint.json"
	}
	dir := filepath.Join(home, ".config", "dprint")
	path := filepath.Join(dir, "dprint.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(path, []byte(defaultDprintConfig), 0644)
	return path
}

const defaultDprintConfig = `{
  "typescript": {},
  "json": {},
  "markdown": {},
  "toml": {},
  "yaml": {},
  "ruff": {},
  "plugins": [
    "https://plugins.dprint.dev/typescript-0.95.15.wasm",
    "https://plugins.dprint.dev/json-0.21.3.wasm",
    "https://plugins.dprint.dev/markdown-0.21.1.wasm",
    "https://plugins.dprint.dev/toml-0.7.0.wasm",
    "https://plugins.dprint.dev/g-plane/pretty_yaml-v0.6.0.wasm",
    "https://plugins.dprint.dev/ruff-0.7.11.wasm"
  ]
}
`

// binaryFileRefusal sniffs the first 8KB of path for binary indicators. NUL
// bytes are the strong signal (compiled executables, archives, images). We
// also flag content that is not valid UTF-8 and has a high ratio of control
// bytes (excluding tab/CR/LF) — that catches binary formats that happen to
// have no NULs in their header, like some compressed streams.
//
// Refuse up front with a one-line message that names size, so the model can
// decide whether to use a deliberate tool (strings, hexdump via exec) instead.
func binaryFileRefusal(abs string) (string, bool) {
	f, err := os.Open(abs)
	if err != nil {
		return "", false
	}
	defer f.Close()
	var buf [8192]byte
	n, _ := f.Read(buf[:])
	if n == 0 {
		return "", false
	}
	sample := buf[:n]
	hasNUL := false
	ctrl := 0
	for _, b := range sample {
		switch {
		case b == 0:
			hasNUL = true
		case b < 0x09, b == 0x0B, b == 0x0C, (b > 0x0D && b < 0x20), b == 0x7F:
			ctrl++
		}
	}
	// Decision tree:
	//   NUL byte                                → binary (strong signal).
	//   ctrl-byte ratio > 10% of sample         → binary. Bytes like
	//       0x01..0x08 are technically valid single-byte ASCII per
	//       utf8.Valid, so we cannot rely on UTF-8 validity alone — a
	//       sample full of 0x01..0x08 still passes utf8.Valid but is
	//       obviously not text.
	//   invalid UTF-8 AND any ctrl bytes        → binary. Catches binary
	//       streams that happen to be sub-10% control but are clearly not
	//       text encodings.
	// Files that are valid UTF-8 with no control bytes — including Latin-1
	// content that happens to also be valid UTF-8 — pass through.
	binary := hasNUL || ctrl*100 > n*10 || (!utf8.Valid(sample) && ctrl > 0)
	if !binary {
		return "", false
	}
	fi, _ := f.Stat()
	size := int64(-1)
	if fi != nil {
		size = fi.Size()
	}
	return fmt.Sprintf("[binary file refused: %s (%d bytes). Loading binary content into context burns tokens with no value. If you need bytes, use exec with `strings`, `xxd`, or `file` deliberately.]", abs, size), true
}

// limitBuf is a write-capped buffer used to bound tool output size.
type limitBuf struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitBuf) Write(p []byte) (int, error) {
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.truncated = true
		b.buf.Write(p[:remaining])
		return len(p), nil
	}
	return b.buf.Write(p)
}

// requireReason returns an error if the LLM omitted the justification field.
func requireReason(r string) error {
	if strings.TrimSpace(r) == "" {
		return fmt.Errorf("reason is required: state in one sentence why you are calling this tool")
	}
	return nil
}
