// Package llm is the model layer: the wire types a completion is made of,
// the OpenAI-compatible streaming client, and the configuration that selects
// which endpoint to talk to.
//
// BOUNDARY RULE: llm sits directly above config and may import it. It must
// never import session, tools, or agent. In particular it does not know what
// a Tool is — only ToolSpec, the flat {name, description, schema} shape a
// provider actually needs on the wire. Keeping Fn out of this package is what
// stops the model layer from depending on the execution layer.
package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/atakang7/axon/internal/config"
)

// Provider is one resolved (endpoint, model) pair: everything needed to send
// a completion request. Extra carries provider-specific routing options and
// is forwarded verbatim as the request's "provider" field.
type Provider struct {
	Name, BaseURL, Model, APIKey string
	Extra                        json.RawMessage
}

// ---------------------------------------------------------------------------
// providers.json
// ---------------------------------------------------------------------------

// LoadProviders reads providers.json and returns a map keyed by
// "provider/model". A provider entry may carry several models under "models";
// the older single "model" field is still accepted so existing configs keep
// working. A missing file is not an error — env-only configuration is valid.
//
//	{
//	  "providers": [
//	    {
//	      "name": "openrouter",
//	      "base_url": "https://openrouter.ai/api",
//	      "api_key": "sk-...",
//	      "models": [{"model": "anthropic/claude-sonnet-4-6"}]
//	    }
//	  ]
//	}
func LoadProviders() (map[string]Provider, error) {
	out := map[string]Provider{}
	data, err := os.ReadFile(config.ProvidersPath())
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Providers []struct {
			Name     string          `json:"name"`
			BaseURL  string          `json:"base_url"`
			Model    string          `json:"model"`
			APIKey   string          `json:"api_key"`
			Provider json.RawMessage `json:"provider"`
			Models   []struct {
				Model    string          `json:"model"`
				Alias    string          `json:"alias,omitempty"`
				Provider json.RawMessage `json:"provider,omitempty"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	for _, p := range cfg.Providers {
		if p.Name == "" {
			return nil, fmt.Errorf("provider name required")
		}
		name := strings.ToLower(p.Name)
		defaultExtra := normalizeExtra(p.Provider)
		models := p.Models
		if len(models) == 0 && p.Model != "" {
			models = append(models, struct {
				Model    string          `json:"model"`
				Alias    string          `json:"alias,omitempty"`
				Provider json.RawMessage `json:"provider,omitempty"`
			}{Model: p.Model})
		}
		if len(models) == 0 {
			return nil, fmt.Errorf("provider %q has no models", name)
		}
		for _, m := range models {
			if m.Model == "" {
				return nil, fmt.Errorf("provider %q has a model entry with no model field", name)
			}
			extra := normalizeExtra(m.Provider)
			if extra == nil {
				extra = defaultExtra
			}
			out[name+"/"+m.Model] = Provider{
				Name: name, BaseURL: p.BaseURL, Model: m.Model, APIKey: p.APIKey, Extra: extra,
			}
		}
	}
	return out, nil
}

// normalizeExtra expands the `provider` field. A bare string (e.g.
// "anthropic") is the OpenRouter routing-slug shorthand and becomes the full
// {order, allow_fallbacks} object. Anything else passes through untouched.
func normalizeExtra(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "\"") {
		return raw
	}
	var slug string
	if err := json.Unmarshal(raw, &slug); err != nil {
		return raw
	}
	if slug == "" {
		return nil
	}
	expanded, _ := json.Marshal(map[string]any{"order": []string{slug}, "allow_fallbacks": true})
	return expanded
}

// ---------------------------------------------------------------------------
// Selection
// ---------------------------------------------------------------------------

// ErrAmbiguousProvider signals that more than one (provider, model) pair is
// configured and no LLM_PROVIDER selector was given. Embedders catch this and
// run an interactive picker.
var ErrAmbiguousProvider = fmt.Errorf("multiple provider/model pairs configured")

// ResolveProvider picks one (provider, model) pair. Resolution order:
//
//  1. LLM_PROVIDER — a full "provider/model" key, or just the provider name
//     when exactly one model is configured for it.
//  2. A single configured pair — use it.
//  3. Pure-env config (LLM_MODEL + LLM_BASE_URL, no providers.json).
//  4. Otherwise ErrAmbiguousProvider, so the embedder can prompt.
//
// Environment values always override the chosen entry, so LLM_MODEL can
// retarget a configured provider without editing the file.
func ResolveProvider(providers map[string]Provider) (Provider, error) {
	if sel := config.String("LLM_PROVIDER"); sel != "" {
		if p, ok := providers[strings.ToLower(sel)]; ok {
			return ApplyEnvOverrides(p)
		}
		matches := providersByName(providers, sel)
		if len(matches) == 1 {
			return ApplyEnvOverrides(providers[matches[0]])
		}
		if len(matches) > 1 {
			return Provider{}, fmt.Errorf("LLM_PROVIDER=%q is ambiguous; use one of: %s", sel, strings.Join(matches, ", "))
		}
		if p, ok, err := providerFromEnv(); err != nil {
			return Provider{}, err
		} else if ok && strings.EqualFold(p.Name, sel) {
			return p, nil
		}
		return Provider{}, fmt.Errorf("provider %q not found in %s", sel, config.ProvidersPath())
	}
	if len(providers) == 1 {
		for _, p := range providers {
			return ApplyEnvOverrides(p)
		}
	}
	if p, ok, err := providerFromEnv(); err != nil {
		return Provider{}, err
	} else if ok {
		return p, nil
	}
	if len(providers) == 0 {
		return Provider{}, fmt.Errorf("no provider configured; set LLM_MODEL and LLM_BASE_URL or create %s", config.ProvidersPath())
	}
	return Provider{}, ErrAmbiguousProvider
}

// providerFromEnv builds a Provider entirely from LLM_* variables. Reports
// ok=false when no LLM_* variable is set at all, so callers can fall through
// to file configuration.
func providerFromEnv() (Provider, bool, error) {
	model := config.String("LLM_MODEL")
	baseURL := config.String("LLM_BASE_URL")
	apiKey := config.String("LLM_API_KEY")
	extraText := config.String("LLM_PROVIDER_EXTRA")
	if model == "" && baseURL == "" && apiKey == "" && extraText == "" {
		return Provider{}, false, nil
	}
	if model == "" || baseURL == "" {
		return Provider{}, false, fmt.Errorf("LLM_MODEL and LLM_BASE_URL are required when provider config is supplied via env")
	}
	name := config.String("LLM_PROVIDER_NAME")
	if name == "" {
		name = config.String("LLM_PROVIDER")
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

// ApplyEnvOverrides layers LLM_BASE_URL / LLM_MODEL / LLM_API_KEY /
// LLM_PROVIDER_EXTRA on top of p. Exported because an embedder running its own
// provider picker must apply the same overrides the runtime would.
func ApplyEnvOverrides(p Provider) (Provider, error) {
	if baseURL := config.String("LLM_BASE_URL"); baseURL != "" {
		p.BaseURL = baseURL
	}
	if model := config.String("LLM_MODEL"); model != "" {
		p.Model = model
	}
	if apiKey := config.String("LLM_API_KEY"); apiKey != "" {
		p.APIKey = apiKey
	}
	if extraText := config.String("LLM_PROVIDER_EXTRA"); extraText != "" {
		if !json.Valid([]byte(extraText)) {
			return Provider{}, fmt.Errorf("LLM_PROVIDER_EXTRA must be valid JSON")
		}
		p.Extra = json.RawMessage(extraText)
	}
	return p, nil
}

// ProviderNames returns the sorted keys of a providers map.
func ProviderNames(providers map[string]Provider) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// providersByName returns every key whose provider-name segment matches sel,
// supporting the `LLM_PROVIDER=openrouter` shorthand.
func providersByName(providers map[string]Provider, sel string) []string {
	var out []string
	for key, p := range providers {
		if strings.EqualFold(p.Name, sel) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
