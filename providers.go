package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ProviderConfig struct{ Name, BaseURL, Model, APIKey string }

type ProviderRegistry struct{ providers map[string]ProviderConfig }

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{providers: map[string]ProviderConfig{}}
}

func (r *ProviderRegistry) Register(name, baseURL, model, apiKey string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || strings.TrimSpace(model) == "" {
		return fmt.Errorf("provider name and model are required")
	}
	r.providers[name] = ProviderConfig{name, strings.TrimSpace(baseURL), strings.TrimSpace(model), strings.TrimSpace(apiKey)}
	return nil
}

func (r *ProviderRegistry) Get(name string) (ProviderConfig, bool) {
	p, ok := r.providers[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

func (r *ProviderRegistry) NewClient(name string) (ChatClient, error) {
	p, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("provider %q not registered — add it to ~/.config/agent/providers.json", name)
	}
	return NewOllamaClient(p.BaseURL, p.Model, p.APIKey), nil
}

func LoadConfigFile(registry *ProviderRegistry) error {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".config", "agent", "providers.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var cfg struct {
		Providers []struct {
			Name    string `json:"name"`
			BaseURL string `json:"base_url"`
			Model   string `json:"model"`
			APIKey  string `json:"api_key"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("providers.json: %w", err)
	}
	for _, p := range cfg.Providers {
		if err := registry.Register(p.Name, p.BaseURL, p.Model, p.APIKey); err != nil {
			return err
		}
	}
	return nil
}

func SelectedProviderName(lookup func(string) string) string {
	if name := strings.ToLower(strings.TrimSpace(lookup("LLM_PROVIDER"))); name != "" {
		return name
	}
	return "ollama"
}
