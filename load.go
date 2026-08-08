package axon

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// load.go reads the two files axon runs on.
//
//	axon.yaml   every setting. No secrets, so it can be committed.
//	.env        every secret, and nothing else.
//
// The split is the point. Settings and credentials have different lifecycles,
// different rotation and different blast radius, and only one of them is ever
// safe to paste into a bug report. Keeping them apart means the interesting
// file — the one people actually read and diff — never has to be redacted.
//
// The two meet in exactly one place: a ${VAR} in the config is resolved from
// the credentials file. Nowhere else in axon reads either file.
//
// Loading is not automatic. Nothing in this package calls Load on its own; an
// embedder calls it and passes the result in. That is what keeps two agents in
// one process able to differ, and what keeps a test from having to arrange the
// filesystem to assert on behaviour.

// DocsURL is where an operator is sent when configuration is missing or
// wrong. It is a constant because every error that can be fixed by reading
// the documentation should say so, in the same place, every time.
const DocsURL = "https://github.com/atakang7/axon#configuration"

// Configuration errors. Check with errors.Is at the boundary; the wrapped
// message carries the path and the fix.
var (
	ErrMissingConfig   = errors.New("axon: configuration file not found")
	ErrMissingEnv      = errors.New("axon: credentials file not found")
	ErrInvalidConfig   = errors.New("axon: invalid configuration")
	ErrUnknownProvider = errors.New("axon: unknown provider")
	ErrUnknownModel    = errors.New("axon: unknown model")
)

// ---------------------------------------------------------------------------
// Where the files live
// ---------------------------------------------------------------------------

// ConfigPath returns the settings file: $AXON_CONFIG, else
// $XDG_CONFIG_HOME/axon/axon.yaml, else ~/.config/axon/axon.yaml.
func ConfigPath() string {
	if path := String("AXON_CONFIG"); path != "" {
		return path
	}

	return filepath.Join(configHome(), "axon", "axon.yaml")
}

// EnvPath returns the credentials file: $AXON_ENV, else beside the config.
//
// It defaults next to axon.yaml rather than to the working directory so that
// moving between projects cannot silently change which credentials are used.
func EnvPath() string {
	if path := String("AXON_ENV"); path != "" {
		return path
	}

	return filepath.Join(configHome(), "axon", ".env")
}

func configHome() string {
	if dir := String("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}

	return filepath.Join(homeDir(), ".config")
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

// Load reads the configuration from the standard locations. Both files are
// required; a missing one is an error naming the path and the documentation.
func Load() (Settings, error) {
	return LoadFrom(ConfigPath(), EnvPath())
}

// LoadFrom reads the configuration from explicit paths.
//
// The returned Settings has defaults applied and every ${VAR} resolved, so
// nothing about it is still pending when it comes back.
func LoadFrom(configPath, envPath string) (Settings, error) {
	secrets, err := readEnvFile(envPath)
	if err != nil {
		return Settings{}, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Settings{}, fmt.Errorf("%w at %s\n\nWrite one, or copy the example from %s",
				ErrMissingConfig, configPath, DocsURL)
		}

		return Settings{}, fmt.Errorf("axon: read %s: %w", configPath, err)
	}

	var settings Settings

	// KnownFields turns a typo into an error instead of a setting that
	// silently does nothing — the single most common way a config file lies
	// to the person who wrote it.
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)

	if err := decoder.Decode(&settings); err != nil {
		return Settings{}, fmt.Errorf("%w: %s: %w\n\nSee %s", ErrInvalidConfig, configPath, err, DocsURL)
	}

	if err := settings.resolveSecrets(secrets, configPath); err != nil {
		return Settings{}, err
	}

	settings = settings.WithDefaults()

	if err := settings.validate(configPath); err != nil {
		return Settings{}, err
	}

	return settings, nil
}

// validate rejects a config that parsed but cannot work, at load time rather
// than at the first request.
func (r Settings) validate(configPath string) error {
	if len(r.Providers) == 0 {
		return fmt.Errorf("%w: %s defines no providers\n\nSee %s", ErrInvalidConfig, configPath, DocsURL)
	}

	for name, e := range r.Providers {
		if e.BaseURL == "" {
			return fmt.Errorf("%w: provider %q has no base_url (%s)", ErrInvalidConfig, name, configPath)
		}

		if len(e.Models) == 0 {
			return fmt.Errorf("%w: provider %q lists no models (%s)", ErrInvalidConfig, name, configPath)
		}
	}

	if r.Retry.MaxAttempts < 1 {
		return fmt.Errorf("%w: retry.max_attempts must be at least 1 (%s)", ErrInvalidConfig, configPath)
	}

	// Caught here rather than at prune time: a typo'd mode would otherwise
	// silently fall back to the default and quietly not do what was asked,
	// hours into a session.
	if m := r.Pruner.Mode; m != "" && !m.Valid() {
		return fmt.Errorf("%w: pruner.mode %q is not one of %s (%s)",
			ErrInvalidConfig, m, joinModes(), configPath)
	}

	return nil
}

func joinModes() string {
	names := make([]string, 0, len(PruneModes))
	for _, m := range PruneModes {
		names = append(names, string(m))
	}
	return join(names)
}

// resolveSecrets replaces every ${VAR} in the provider section with its value
// from the credentials file.
//
// Only the provider fields are expanded. A config is mostly numbers and
// durations, and expanding everything would mean a stray "$" in some future
// string field became a confusing failure.
func (r Settings) resolveSecrets(secrets map[string]string, configPath string) error {
	for name, e := range r.Providers {
		key, err := expand(e.APIKey, secrets)
		if err != nil {
			return fmt.Errorf("%w: provider %q: %w (%s)\n\nAdd it to %s or see %s",
				ErrInvalidConfig, name, err, configPath, EnvPath(), DocsURL)
		}
		e.APIKey = key

		url, err := expand(e.BaseURL, secrets)
		if err != nil {
			return fmt.Errorf("%w: provider %q: %w (%s)", ErrInvalidConfig, name, err, configPath)
		}
		e.BaseURL = url

		r.Providers[name] = e
	}

	return nil
}

// expand resolves ${VAR} and $VAR references against the credentials.
//
// An unresolved reference is an error, not an empty string. An empty API key
// reaches the provider as a missing Authorization header and comes back as a
// 401 several seconds later, which is a much worse way to learn that a
// variable name was misspelled.
func expand(value string, secrets map[string]string) (string, error) {
	if !strings.Contains(value, "$") {
		return value, nil
	}

	var (
		out     strings.Builder
		runes   = []rune(value)
		missing []string
	)

	for i := 0; i < len(runes); i++ {
		if runes[i] != '$' {
			out.WriteRune(runes[i])

			continue
		}

		name, width := readVarName(runes[i+1:])
		if name == "" {
			out.WriteRune('$')

			continue
		}
		i += width

		if v, ok := secrets[name]; ok {
			out.WriteString(v)

			continue
		}

		// Fall back to the process environment, so a CI runner that injects
		// secrets as real variables works without writing a file.
		if v, ok := os.LookupEnv(name); ok && strings.TrimSpace(v) != "" {
			out.WriteString(v)

			continue
		}

		missing = append(missing, name)
	}

	if len(missing) > 0 {
		return "", fmt.Errorf("%s is not set", strings.Join(missing, ", "))
	}

	return out.String(), nil
}

// readVarName reads a ${NAME} or $NAME reference, returning the name and how
// many runes it consumed after the dollar sign.
func readVarName(rest []rune) (string, int) {
	if len(rest) == 0 {
		return "", 0
	}

	if rest[0] == '{' {
		for i := 1; i < len(rest); i++ {
			if rest[i] == '}' {
				return string(rest[1:i]), i + 1
			}
		}

		return "", 0
	}

	end := 0
	for end < len(rest) && (rest[end] == '_' ||
		(rest[end] >= 'A' && rest[end] <= 'Z') ||
		(rest[end] >= 'a' && rest[end] <= 'z') ||
		(rest[end] >= '0' && rest[end] <= '9')) {
		end++
	}

	if end == 0 {
		return "", 0
	}

	return string(rest[:end]), end
}

// ---------------------------------------------------------------------------
// The credentials file
// ---------------------------------------------------------------------------

// readEnvFile parses a .env file into a map.
//
// The format is the conventional one: KEY=VALUE per line, # comments, blank
// lines ignored, an optional `export ` prefix, and optional quotes around the
// value. It is deliberately not a shell — no command substitution, no
// interpolation between entries — because a credentials file that can execute
// is a credentials file that can be weaponised.
func readEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w at %s\n\nCreate it with your API key, e.g.\n\n    OPENROUTER_API_KEY=sk-or-...\n\nSee %s",
				ErrMissingEnv, path, DocsURL)
		}

		return nil, fmt.Errorf("axon: read %s: %w", path, err)
	}
	defer file.Close()

	secrets := map[string]string{}

	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		key, value, ok := parseEnvLine(scanner.Text())
		if !ok {
			continue
		}

		if key == "" {
			return nil, fmt.Errorf("%w: %s:%d has a value with no name", ErrInvalidConfig, path, line)
		}

		secrets[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("axon: read %s: %w", path, err)
	}

	return secrets, nil
}

// parseEnvLine splits one line, reporting false for blanks and comments.
func parseEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	line = strings.TrimPrefix(line, "export ")

	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}

	return strings.TrimSpace(key), unquote(strings.TrimSpace(value)), true
}

// unquote strips one matching pair of surrounding quotes, so a value with
// trailing spaces can be written explicitly.
func unquote(v string) string {
	if len(v) < 2 {
		return v
	}

	if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}

	return v
}

// quoteJSON renders a string as a JSON string literal, escaping the two
// characters that can appear in a provider name and break the encoding.
func quoteJSON(s string) string {
	var b strings.Builder

	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)

		case '\\':
			b.WriteString(`\\`)

		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')

	return b.String()
}
