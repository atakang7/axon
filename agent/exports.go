package agent

// exports.go — small surface of internal helpers that the reference CLI
// (cmd/axon) needs. These wrap unexported config/env logic so the CLI
// can resolve paths and apply env overrides without re-implementing
// XDG/AXON_* conventions. Not part of the core embedding API.

// DataDir returns the on-disk directory the runtime uses for session
// files and CLI-persisted state (e.g. last-chosen provider). Honors
// AXON_DATA_DIR, XDG_DATA_HOME, then defaults to ~/.local/share/agent.
func DataDir() string { return dataDir() }

// ConfigDir returns the on-disk directory where the runtime looks for
// providers.json by default. Honors AXON_CONFIG_DIR, XDG_CONFIG_HOME,
// then defaults to ~/.config/agent.
func ConfigDir() string { return configDir() }

// ProvidersPath returns the path the runtime reads providers from.
// Honors AXON_PROVIDERS_PATH, otherwise ConfigDir()/providers.json.
func ProvidersPath() string { return providersPath() }

// SessionPath returns the path the runtime persists the current session
// to. Honors AXON_SESSION_PATH; otherwise derives a per-cwd path under
// DataDir().
func SessionPath() string { return sessionPath() }

// ApplyProviderEnvOverrides applies LLM_BASE_URL / LLM_MODEL / LLM_API_KEY /
// LLM_PROVIDER_EXTRA on top of the given Provider, returning the result.
// Used by the CLI picker so that env values still override the chosen
// config entry.
func ApplyProviderEnvOverrides(p Provider) (Provider, error) {
	return applyProviderEnvOverrides(p)
}

// EnvString returns the trimmed value of the given environment variable.
// Used by the CLI for the same env-resolution semantics the runtime uses
// (notably trimming, which os.Getenv alone does not do).
func EnvString(key string) string { return envString(key) }

// ProviderNames returns the sorted keys of a providers map.
func ProviderNames(providers map[string]Provider) []string {
	return providerNames(providers)
}
