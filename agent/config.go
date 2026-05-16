package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func envString(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func envInt(key string, fallback, min int) int {
	raw := envString(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min {
		return fallback
	}
	return n
}

func userHomeDir() string {
	home, _ := os.UserHomeDir()
	if home != "" {
		return home
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

func configDir() string {
	if dir := envString("AXON_CONFIG_DIR"); dir != "" {
		return dir
	}
	if dir := envString("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "agent")
	}
	return filepath.Join(userHomeDir(), ".config", "agent")
}

func dataDir() string {
	if dir := envString("AXON_DATA_DIR"); dir != "" {
		return dir
	}
	if dir := envString("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "agent")
	}
	return filepath.Join(userHomeDir(), ".local", "share", "agent")
}

func providersPath() string {
	if path := envString("AXON_PROVIDERS_PATH"); path != "" {
		return path
	}
	return filepath.Join(configDir(), "providers.json")
}

func sessionPath() string {
	if path := envString("AXON_SESSION_PATH"); path != "" {
		return path
	}
	return filepath.Join(dataDir(), "sessions", sessionKeyForCwd()+".json")
}

// sessionKeyForCwd derives a stable per-directory key so each working
// directory keeps its own session. Falls back to "default" if cwd is
// unavailable, preserving the old single-session behavior in that case.
func sessionKeyForCwd() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return "default"
	}
	if abs, err := filepath.Abs(wd); err == nil {
		wd = abs
	}
	sum := sha256.Sum256([]byte(wd))
	return filepath.Base(wd) + "-" + hex.EncodeToString(sum[:6])
}

func readLimit() int {
	return envInt("AXON_READ_LIMIT", 200, 1)
}

// readMaxBytes caps mode=full reads. A 50MB log file would otherwise be
// loaded into memory and split line-by-line into context. Default 2 MiB.
func readMaxBytes() int {
	return envInt("AXON_READ_MAX_BYTES", 2*1024*1024, 1)
}

func execTimeoutSeconds() int {
	return envInt("AXON_EXEC_TIMEOUT_SECONDS", 30, 1)
}

// execMaxTimeoutSeconds caps any per-call timeout the LLM supplies. Without a
// ceiling a single tool call could hold the turn for hours. Default 600s (10m).
func execMaxTimeoutSeconds() int {
	return envInt("AXON_EXEC_MAX_TIMEOUT_SECONDS", 600, 1)
}

// bashOutputMaxBytes caps a single bash_output read so a noisy server
// can't dump megabytes into context per poll. Default 32 KiB.
func bashOutputMaxBytes() int {
	return envInt("AXON_BASH_OUTPUT_MAX_BYTES", 32*1024, 1)
}

// searchTimeoutSeconds bounds rg per call. Default 30s.
func searchTimeoutSeconds() int {
	return envInt("AXON_SEARCH_TIMEOUT_SECONDS", 30, 1)
}

// searchLimit caps the number of matches a single search returns. Default 100.
func searchLimit() int {
	return envInt("AXON_SEARCH_LIMIT", 100, 1)
}

func execOutputLimit() int {
	return envInt("AXON_EXEC_OUTPUT_LIMIT", 12000, 1)
}

func searchOutputLimit() int {
	return envInt("AXON_SEARCH_OUTPUT_LIMIT", 12000, 1)
}

func execDefaultTailLines() int {
	return envInt("AXON_EXEC_TAIL_LINES", 50, 1)
}

func execMaxTailLines() int {
	return envInt("AXON_EXEC_MAX_TAIL_LINES", 500, 1)
}

func providerFromEnv() (Provider, bool, error) {
	model := envString("LLM_MODEL")
	baseURL := envString("LLM_BASE_URL")
	apiKey := envString("LLM_API_KEY")
	extraText := envString("LLM_PROVIDER_EXTRA")
	if model == "" && baseURL == "" && apiKey == "" && extraText == "" {
		return Provider{}, false, nil
	}
	if model == "" || baseURL == "" {
		return Provider{}, false, fmt.Errorf("LLM_MODEL and LLM_BASE_URL are required when provider config is supplied via env")
	}
	name := envString("LLM_PROVIDER_NAME")
	if name == "" {
		name = envString("LLM_PROVIDER")
	}
	if name == "" {
		name = "env"
	}
	var extra json.RawMessage
	if extraText != "" {
		if !json.Valid([]byte(extraText)) {
			return Provider{}, false, fmt.Errorf("LLM_PROVIDER_EXTRA must be valid JSON")
		}
		extra = json.RawMessage(extraText)
	}
	return Provider{
		Name:    strings.ToLower(name),
		BaseURL: baseURL,
		Model:   model,
		APIKey:  apiKey,
		Extra:   extra,
	}, true, nil
}

func applyProviderEnvOverrides(p Provider) (Provider, error) {
	if baseURL := envString("LLM_BASE_URL"); baseURL != "" {
		p.BaseURL = baseURL
	}
	if model := envString("LLM_MODEL"); model != "" {
		p.Model = model
	}
	if apiKey := envString("LLM_API_KEY"); apiKey != "" {
		p.APIKey = apiKey
	}
	if extraText := envString("LLM_PROVIDER_EXTRA"); extraText != "" {
		if !json.Valid([]byte(extraText)) {
			return Provider{}, fmt.Errorf("LLM_PROVIDER_EXTRA must be valid JSON")
		}
		p.Extra = json.RawMessage(extraText)
	}
	return p, nil
}

func providerNames(providers map[string]Provider) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolveProvider picks a (provider, model) pair. Resolution order:
//  1. LLM_PROVIDER env — full "provider/model" key, or just provider name when
//     it has exactly one model configured.
//  2. Single configured pair → use it.
//  3. Pure-env config (LLM_MODEL + LLM_BASE_URL with no providers.json).
//  4. Otherwise return ErrAmbiguousProvider so main.go can run the picker.
func ResolveProvider(providers map[string]Provider) (Provider, error) {
	if sel := strings.TrimSpace(envString("LLM_PROVIDER")); sel != "" {
		if p, ok := providers[strings.ToLower(sel)]; ok {
			return applyProviderEnvOverrides(p)
		}
		matches := providersByName(providers, sel)
		if len(matches) == 1 {
			return applyProviderEnvOverrides(providers[matches[0]])
		}
		if len(matches) > 1 {
			return Provider{}, fmt.Errorf("LLM_PROVIDER=%q is ambiguous; use one of: %s", sel, strings.Join(matches, ", "))
		}
		if p, ok, err := providerFromEnv(); err != nil {
			return Provider{}, err
		} else if ok && strings.EqualFold(p.Name, sel) {
			return p, nil
		}
		return Provider{}, fmt.Errorf("provider %q not found in %s", sel, providersPath())
	}
	if len(providers) == 1 {
		for _, p := range providers {
			return applyProviderEnvOverrides(p)
		}
	}
	if p, ok, err := providerFromEnv(); err != nil {
		return Provider{}, err
	} else if ok {
		return p, nil
	}
	if len(providers) == 0 {
		return Provider{}, fmt.Errorf("no provider configured; set LLM_MODEL and LLM_BASE_URL or create %s", providersPath())
	}
	return Provider{}, ErrAmbiguousProvider
}

// ErrAmbiguousProvider signals that more than one (provider, model) pair is
// configured and no LLM_PROVIDER selector was given. main.go catches this
// and runs the interactive picker.
var ErrAmbiguousProvider = fmt.Errorf("multiple provider/model pairs configured")

// providersByName returns all keys whose provider-name segment matches sel.
// Used for `LLM_PROVIDER=openrouter` shorthand when only one model exists.
func providersByName(providers map[string]Provider, sel string) []string {
	sel = strings.ToLower(sel)
	var out []string
	for key, p := range providers {
		if strings.EqualFold(p.Name, sel) {
			out = append(out, key)
		}
		_ = key
	}
	sort.Strings(out)
	return out
}
