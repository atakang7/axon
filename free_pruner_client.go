package axon

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// NewFreePrunerClient never routes to a paid model or endpoint.
func (s Settings) NewFreePrunerClient(endpoint, model string) (Model, error) {
	p, err := s.Provider(endpoint, model)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Scheme != "https" || u.Host != "openrouter.ai" || !strings.HasSuffix(model, ":free") {
		return nil, fmt.Errorf("pruner requires a :free model on HTTPS OpenRouter; paid models are only available for the main agent")
	}
	extra := map[string]any{}
	if len(p.Extra) > 0 {
		if err := json.Unmarshal(p.Extra, &extra); err != nil {
			return nil, err
		}
	}
	if extra == nil {
		extra = map[string]any{}
	}
	extra["max_price"] = map[string]float64{"prompt": 0, "completion": 0, "request": 0}
	extra["require_parameters"] = true
	p.Extra, _ = json.Marshal(extra)
	return NewClient(ClientConfig{Provider: p, MaxTokens: s.Pruner.MaxTokens})
}
