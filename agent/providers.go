package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// providers.go — Provider struct + LoadProviders config loader.

type Provider struct {
	Name, BaseURL, Model, APIKey string
	Extra                        json.RawMessage
}

func LoadProviders() (map[string]Provider, error) {
	out := map[string]Provider{}
	data, err := os.ReadFile(providersPath())
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
	// providerExtra normalises the `provider` field. A bare string (e.g.
	// "anthropic") is the OpenRouter routing slug shorthand and gets expanded
	// to the full {order, allow_fallbacks} object. Anything else passes through.
	providerExtra := func(raw json.RawMessage) json.RawMessage {
		if len(raw) == 0 {
			return nil
		}
		t := strings.TrimSpace(string(raw))
		if strings.HasPrefix(t, "\"") {
			var slug string
			if err := json.Unmarshal(raw, &slug); err == nil {
				if slug == "" {
					return nil
				}
				out, _ := json.Marshal(map[string]any{"order": []string{slug}, "allow_fallbacks": true})
				return out
			}
		}
		return raw
	}
	for _, p := range cfg.Providers {
		if p.Name == "" {
			return nil, fmt.Errorf("provider name required")
		}
		name := strings.ToLower(p.Name)
		defaultExtra := providerExtra(p.Provider)
		models := p.Models
		if len(models) == 0 && p.Model != "" {
			models = []struct {
				Model    string          `json:"model"`
				Alias    string          `json:"alias,omitempty"`
				Provider json.RawMessage `json:"provider,omitempty"`
			}{{Model: p.Model}}
		}
		if len(models) == 0 {
			return nil, fmt.Errorf("provider %q has no models", name)
		}
		for _, m := range models {
			if m.Model == "" {
				return nil, fmt.Errorf("provider %q has a model entry with no model field", name)
			}
			extra := providerExtra(m.Provider)
			if extra == nil {
				extra = defaultExtra
			}
			key := name + "/" + m.Model
			out[key] = Provider{Name: name, BaseURL: p.BaseURL, Model: m.Model, APIKey: p.APIKey, Extra: extra}
		}
	}
	return out, nil
}
