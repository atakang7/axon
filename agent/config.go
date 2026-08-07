package agent

// config.go — provider selection.
//
// Path and limit resolution moved to internal/config; this file now holds
// only the logic that turns environment variables plus providers.json into
// one concrete Provider. It belongs beside the LLM client, and moves there
// when the llm package lands.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atakang7/axon/internal/config"
)

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

func applyProviderEnvOverrides(p Provider) (Provider, error) {
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
//  4. Otherwise return ErrAmbiguousProvider so the embedder can run a picker.
func ResolveProvider(providers map[string]Provider) (Provider, error) {
	if sel := config.String("LLM_PROVIDER"); sel != "" {
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
		return Provider{}, fmt.Errorf("provider %q not found in %s", sel, config.ProvidersPath())
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
		return Provider{}, fmt.Errorf("no provider configured; set LLM_MODEL and LLM_BASE_URL or create %s", config.ProvidersPath())
	}
	return Provider{}, ErrAmbiguousProvider
}

// ErrAmbiguousProvider signals that more than one (provider, model) pair is
// configured and no LLM_PROVIDER selector was given. Embedders catch this and
// run an interactive picker.
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
	}
	sort.Strings(out)
	return out
}
