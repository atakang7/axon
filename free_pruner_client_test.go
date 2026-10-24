package axon

import (
	"encoding/json"
	"testing"
)

func TestFreePrunerClientRejectsPaidAndCapsEndpointPrice(t *testing.T) {
	s := DefaultSettings()
	s.Providers = map[string]Endpoint{"router": {BaseURL: "https://openrouter.ai/api/v1", Models: map[string]ModelOptions{"vendor/free:free": {}, "vendor/paid": {}}}}
	if _, err := s.NewFreePrunerClient("router", "vendor/paid"); err == nil {
		t.Fatal("paid pruner accepted")
	}
	m, err := s.NewFreePrunerClient("router", "vendor/free:free")
	if err != nil {
		t.Fatal(err)
	}
	var extra struct {
		MaxPrice map[string]float64 `json:"max_price"`
	}
	if err := json.Unmarshal(m.(*Client).cfg.Provider.Extra, &extra); err != nil {
		t.Fatal(err)
	}
	if len(extra.MaxPrice) != 3 {
		t.Fatal("price guard missing")
	}
	for _, v := range extra.MaxPrice {
		if v != 0 {
			t.Fatal("paid endpoint allowed")
		}
	}
	e := s.Providers["router"]
	e.BaseURL = "https://example.com"
	s.Providers["router"] = e
	if _, err := s.NewFreePrunerClient("router", "vendor/free:free"); err == nil {
		t.Fatal("unverified endpoint accepted")
	}
}
