package main

import (
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
	return filepath.Join(dataDir(), "session.json")
}

func readLimit() int {
	return envInt("AXON_READ_LIMIT", 200, 1)
}

func execTimeoutSeconds() int {
	return envInt("AXON_EXEC_TIMEOUT_SECONDS", 30, 1)
}

func execOutputLimit() int {
	return envInt("AXON_EXEC_OUTPUT_LIMIT", 12000, 1)
}

func searchLimit() int {
	return envInt("AXON_SEARCH_LIMIT", 100, 1)
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

func ResolveProvider(providers map[string]Provider) (Provider, error) {
	if name := strings.ToLower(envString("LLM_PROVIDER")); name != "" {
		p, ok := providers[name]
		if ok {
			return applyProviderEnvOverrides(p)
		}
		if p, ok, err := providerFromEnv(); err != nil {
			return Provider{}, err
		} else if ok && strings.ToLower(p.Name) == name {
			return p, nil
		}
		return Provider{}, fmt.Errorf("provider %q not found in %s", name, providersPath())
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
	return Provider{}, fmt.Errorf("multiple providers configured (%s); set LLM_PROVIDER", strings.Join(providerNames(providers), ", "))
}
