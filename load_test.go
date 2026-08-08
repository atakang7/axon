package axon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The config is the one part of axon an operator edits by hand, which makes
// the parser the part most likely to fail in someone else's terminal rather
// than here. These tests exercise the real files on disk, not a struct built
// in memory.

// workspace writes both files into a temp directory and returns their paths.
// Passing "" for either skips writing it, which is how the missing-file cases
// are set up.
func workspace(t *testing.T, config, env string) (configPath, envPath string) {
	t.Helper()

	dir := t.TempDir()
	configPath = filepath.Join(dir, "axon.yaml")
	envPath = filepath.Join(dir, ".env")

	if config != "" {
		if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if env != "" {
		if err := os.WriteFile(envPath, []byte(env), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return configPath, envPath
}

const minimalConfig = `
providers:
  openrouter:
    base_url: https://openrouter.ai/api
    api_key: ${OPENROUTER_API_KEY}
    models:
      deepseek/deepseek-v3.2:
`

// ---------------------------------------------------------------------------
// The happy path
// ---------------------------------------------------------------------------

func TestLoadResolvesSecretsAndAppliesDefaults(t *testing.T) {
	cfg, env := workspace(t, minimalConfig, "OPENROUTER_API_KEY=sk-or-secret\n")

	settings, err := LoadFrom(cfg, env)
	if err != nil {
		t.Fatal(err)
	}

	if got := settings.Providers["openrouter"].APIKey; got != "sk-or-secret" {
		t.Errorf("api key = %q, want it resolved from the .env", got)
	}

	// Nothing in the file mentioned these, so every one must come from
	// DefaultSettings rather than being left at zero.
	if settings.Tools.Read.Lines != DefaultSettings().Tools.Read.Lines {
		t.Errorf("read.lines = %d, want the default", settings.Tools.Read.Lines)
	}
	if settings.Retry.MaxAttempts != DefaultSettings().Retry.MaxAttempts {
		t.Errorf("retry.max_attempts = %d, want the default", settings.Retry.MaxAttempts)
	}
	if len(settings.Retry.OnStatus) == 0 {
		t.Error("retry.on_status is empty, want the default list")
	}
}

// A file that sets one field must keep the defaults for every other. This is
// the property that lets a config stay short.
func TestPartialConfigKeepsDefaultsForEverythingElse(t *testing.T) {
	cfg, env := workspace(t, minimalConfig+`
tools:
  exec:
    timeout: 90s
`, "OPENROUTER_API_KEY=k\n")

	settings, err := LoadFrom(cfg, env)
	if err != nil {
		t.Fatal(err)
	}

	if got := settings.Tools.Exec.Timeout.Std(); got != 90*time.Second {
		t.Errorf("exec.timeout = %s, want 90s", got)
	}

	// Same section, untouched field.
	if got, want := settings.Tools.Exec.TailLines, DefaultSettings().Tools.Exec.TailLines; got != want {
		t.Errorf("exec.tail_lines = %d, want the default %d", got, want)
	}

	// Different section entirely.
	if got, want := settings.Pruner.FloorTokens, DefaultSettings().Pruner.FloorTokens; got != want {
		t.Errorf("pruner.floor_tokens = %d, want the default %d", got, want)
	}
}

// ---------------------------------------------------------------------------
// Missing files
// ---------------------------------------------------------------------------

// The whole contract with an operator is that a missing file says which file,
// and where to read about it. A bare "not found" is what this replaces.
func TestMissingConfigNamesThePathAndTheDocs(t *testing.T) {
	_, env := workspace(t, "", "K=v\n")

	_, err := LoadFrom(filepath.Join(t.TempDir(), "axon.yaml"), env)
	if !errors.Is(err, ErrMissingConfig) {
		t.Fatalf("err = %v, want ErrMissingConfig", err)
	}

	if !strings.Contains(err.Error(), "axon.yaml") {
		t.Errorf("err = %v, want it to name the missing file", err)
	}
	if !strings.Contains(err.Error(), DocsURL) {
		t.Errorf("err = %v, want it to link to %s", err, DocsURL)
	}
}

func TestMissingEnvNamesThePathAndTheDocs(t *testing.T) {
	cfg, _ := workspace(t, minimalConfig, "")

	_, err := LoadFrom(cfg, filepath.Join(t.TempDir(), ".env"))
	if !errors.Is(err, ErrMissingEnv) {
		t.Fatalf("err = %v, want ErrMissingEnv", err)
	}

	if !strings.Contains(err.Error(), DocsURL) {
		t.Errorf("err = %v, want it to link to %s", err, DocsURL)
	}
}

// ---------------------------------------------------------------------------
// Secrets
// ---------------------------------------------------------------------------

// An unresolved variable must fail at load. Left empty it reaches the provider
// as a missing Authorization header and comes back as a 401 seconds later,
// which is a far worse way to learn that a name was misspelled.
func TestUnresolvedVariableFailsAtLoadAndNamesTheVariable(t *testing.T) {
	cfg, env := workspace(t, minimalConfig, "SOMETHING_ELSE=v\n")

	_, err := LoadFrom(cfg, env)
	if err == nil {
		t.Fatal("want an error for an unresolved ${OPENROUTER_API_KEY}")
	}

	if !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Errorf("err = %v, want it to name the missing variable", err)
	}
}

// CI injects secrets as real environment variables rather than as a file.
func TestVariableFallsBackToTheProcessEnvironment(t *testing.T) {
	cfg, env := workspace(t, minimalConfig, "# nothing here\n")
	t.Setenv("OPENROUTER_API_KEY", "sk-from-ci")

	settings, err := LoadFrom(cfg, env)
	if err != nil {
		t.Fatal(err)
	}

	if got := settings.Providers["openrouter"].APIKey; got != "sk-from-ci" {
		t.Errorf("api key = %q, want the value from the process environment", got)
	}
}

func TestEnvFileFormat(t *testing.T) {
	cfg, env := workspace(t, `
providers:
  p:
    base_url: https://example.test
    api_key: ${PLAIN}
    models:
      m:
`, `
# a comment
PLAIN=plain-value
export EXPORTED=exported-value
QUOTED="quoted value"
SINGLE='single value'
SPACED  =  spaced-value

MALFORMED
`)

	settings, err := LoadFrom(cfg, env)
	if err != nil {
		t.Fatal(err)
	}

	if got := settings.Providers["p"].APIKey; got != "plain-value" {
		t.Errorf("api key = %q, want plain-value", got)
	}

	secrets, err := readEnvFile(env)
	if err != nil {
		t.Fatal(err)
	}

	for key, want := range map[string]string{
		"EXPORTED": "exported-value",
		"QUOTED":   "quoted value",
		"SINGLE":   "single value",
		"SPACED":   "spaced-value",
	} {
		if got := secrets[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	if _, ok := secrets["MALFORMED"]; ok {
		t.Error("a line with no = must be skipped, not stored")
	}
}

// A literal key must survive untouched — not every deployment uses the .env.
func TestLiteralAPIKeyIsLeftAlone(t *testing.T) {
	cfg, env := workspace(t, `
providers:
  p:
    base_url: https://example.test
    api_key: sk-literal
    models:
      m:
`, "UNUSED=x\n")

	settings, err := LoadFrom(cfg, env)
	if err != nil {
		t.Fatal(err)
	}

	if got := settings.Providers["p"].APIKey; got != "sk-literal" {
		t.Errorf("api key = %q, want sk-literal", got)
	}
}

// ---------------------------------------------------------------------------
// Rejecting bad configs
// ---------------------------------------------------------------------------

// A typo must be an error, not a setting that silently does nothing. This is
// the single most common way a config file lies to the person who wrote it.
func TestUnknownFieldIsRejected(t *testing.T) {
	cfg, env := workspace(t, minimalConfig+`
tools:
  exec:
    timeuot: 90s
`, "OPENROUTER_API_KEY=k\n")

	_, err := LoadFrom(cfg, env)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig for a misspelled field", err)
	}

	if !strings.Contains(err.Error(), "timeuot") {
		t.Errorf("err = %v, want it to name the offending field", err)
	}
}

func TestConfigsThatCannotWorkAreRejectedAtLoad(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   string
	}{
		{
			name:   "no providers",
			config: "session:\n  data_dir: /tmp\n",
			want:   "no providers",
		},
		{
			name:   "provider without base_url",
			config: "providers:\n  p:\n    models:\n      m:\n",
			want:   "base_url",
		},
		{
			name:   "provider without models",
			config: "providers:\n  p:\n    base_url: https://example.test\n",
			want:   "models",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, env := workspace(t, tc.config, "K=v\n")

			_, err := LoadFrom(cfg, env)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Resolving a request target
// ---------------------------------------------------------------------------

func TestProviderResolvesEndpointAndModel(t *testing.T) {
	cfg, env := workspace(t, `
providers:
  openrouter:
    base_url: https://openrouter.ai/api
    api_key: ${K}
    models:
      deepseek/deepseek-v3.2:
        route: Baidu Qianfan
      plain/model:
`, "K=sk-x\n")

	settings, err := LoadFrom(cfg, env)
	if err != nil {
		t.Fatal(err)
	}

	p, err := settings.Provider("openrouter", "deepseek/deepseek-v3.2")
	if err != nil {
		t.Fatal(err)
	}

	if p.Model != "deepseek/deepseek-v3.2" || p.APIKey != "sk-x" || p.BaseURL != "https://openrouter.ai/api" {
		t.Errorf("provider = %+v, want the configured endpoint and model", p)
	}

	if !strings.Contains(string(p.Extra), "Baidu Qianfan") {
		t.Errorf("Extra = %s, want the route carried through", p.Extra)
	}

	// A model with no options must send no routing at all, rather than an
	// empty object the provider has to interpret.
	plain, err := settings.Provider("openrouter", "plain/model")
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Extra) != 0 {
		t.Errorf("Extra = %s, want nothing sent for a model with no route", plain.Extra)
	}
}

// An unknown name is caught here, with the alternatives listed, rather than
// surfacing later as a 404 from the provider.
func TestUnknownProviderAndModelListWhatIsAvailable(t *testing.T) {
	cfg, env := workspace(t, minimalConfig, "OPENROUTER_API_KEY=k\n")

	settings, err := LoadFrom(cfg, env)
	if err != nil {
		t.Fatal(err)
	}

	_, err = settings.Provider("openai", "gpt-4o")
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("err = %v, want ErrUnknownProvider", err)
	}
	if !strings.Contains(err.Error(), "openrouter") {
		t.Errorf("err = %v, want it to list the configured providers", err)
	}

	_, err = settings.Provider("openrouter", "nope")
	if !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("err = %v, want ErrUnknownModel", err)
	}
	if !strings.Contains(err.Error(), "deepseek/deepseek-v3.2") {
		t.Errorf("err = %v, want it to list the configured models", err)
	}
}

func TestModelNamesAreStablyOrdered(t *testing.T) {
	e := Endpoint{Models: map[string]ModelOptions{"c": {}, "a": {}, "b": {}}}

	for range 5 {
		got := e.ModelNames()
		if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
			t.Fatalf("ModelNames() = %v, want a stable sorted order", got)
		}
	}
}

// ---------------------------------------------------------------------------
// Scalars
// ---------------------------------------------------------------------------

func TestDurationParsing(t *testing.T) {
	cases := map[string]time.Duration{
		"30s":     30 * time.Second,
		"10m":     10 * time.Minute,
		"1h30m":   90 * time.Minute,
		"500ms":   500 * time.Millisecond,
		"45":      45 * time.Second, // a bare number is seconds
		"\"30s\"": 30 * time.Second,
	}

	for input, want := range cases {
		cfg, env := workspace(t, minimalConfig+"\ntools:\n  exec:\n    timeout: "+input+"\n", "OPENROUTER_API_KEY=k\n")

		settings, err := LoadFrom(cfg, env)
		if err != nil {
			t.Errorf("%s: %v", input, err)

			continue
		}

		if got := settings.Tools.Exec.Timeout.Std(); got != want {
			t.Errorf("timeout: %s parsed as %s, want %s", input, got, want)
		}
	}
}

func TestByteSizeParsing(t *testing.T) {
	cases := map[string]int{
		"12KB":  12000,
		"2MB":   2000000,
		"32KiB": 32 * 1024,
		"1MiB":  1024 * 1024,
		"4096":  4096,
	}

	for input, want := range cases {
		cfg, env := workspace(t, minimalConfig+"\ntools:\n  read:\n    max_bytes: "+input+"\n", "OPENROUTER_API_KEY=k\n")

		settings, err := LoadFrom(cfg, env)
		if err != nil {
			t.Errorf("%s: %v", input, err)

			continue
		}

		if got := settings.Tools.Read.MaxBytes.Int(); got != want {
			t.Errorf("max_bytes: %s parsed as %d, want %d", input, got, want)
		}
	}
}

// A unit nobody meant must be an error rather than a number nobody expected.
func TestNonsenseScalarsAreRejected(t *testing.T) {
	for _, bad := range []string{"tools:\n  exec:\n    timeout: soon\n", "tools:\n  read:\n    max_bytes: enormous\n", "tools:\n  exec:\n    timeout: -5s\n"} {
		cfg, env := workspace(t, minimalConfig+"\n"+bad, "OPENROUTER_API_KEY=k\n")

		if _, err := LoadFrom(cfg, env); err == nil {
			t.Errorf("%q was accepted, want an error", strings.TrimSpace(bad))
		}
	}
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// Every default must be positive. A zero here does not mean "unlimited", it
// means a tool that returns nothing or a retry loop that never runs.
func TestDefaultSettingsAreFullyPopulated(t *testing.T) {
	d := DefaultSettings()

	if d.Model.MaxTokens <= 0 || d.Model.RequestTimeout <= 0 || d.Model.IdleTimeout <= 0 {
		t.Errorf("model defaults incomplete: %+v", d.Model)
	}
	if d.Retry.MaxAttempts < 1 || d.Retry.BackoffCap <= 0 || len(d.Retry.OnStatus) == 0 {
		t.Errorf("retry defaults incomplete: %+v", d.Retry)
	}
	if d.Pruner.FloorTokens <= 0 || d.Pruner.GrowthTokens <= 0 || d.Pruner.MaxTokens <= 0 || d.Pruner.Timeout <= 0 {
		t.Errorf("pruner defaults incomplete: %+v", d.Pruner)
	}

	if d.Model.IdleTimeout >= d.Model.RequestTimeout {
		t.Error("idle_timeout must be shorter than request_timeout, or it can never fire")
	}
}

// WithDefaults must be safe to apply twice — New calls it on config that may
// already have been through Load.
func TestWithDefaultsIsIdempotent(t *testing.T) {
	once := Settings{}.WithDefaults()
	twice := once.WithDefaults()

	if once.Tools.limits() != twice.Tools.limits() {
		t.Error("applying defaults twice changed the result")
	}
	if once.Model != twice.Model || once.Pruner != twice.Pruner {
		t.Error("applying defaults twice changed the result")
	}
}

// The zero Settings is what an embedder that never calls Load gets, so it has
// to produce a working agent.
func TestZeroSettingsAreUsable(t *testing.T) {
	settings := Settings{}.WithDefaults()

	if settings.Tools.Read.Lines <= 0 || settings.Retry.MaxAttempts < 1 {
		t.Fatalf("zero Settings did not default: %+v", settings)
	}
}
