//go:build !api

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// lastChoice persists the user's previously selected providers so subsequent
// runs can offer them as defaults. Stored alongside session data, so wiping
// data resets the choices too.
type lastChoice struct {
	Main   string `json:"main,omitempty"`
	Pruner string `json:"pruner,omitempty"`
}

func lastChoicePath() string {
	return filepath.Join(dataDir(), "last_choice.json")
}

func loadLastChoice() lastChoice {
	var lc lastChoice
	data, err := os.ReadFile(lastChoicePath())
	if err != nil {
		return lc
	}
	_ = json.Unmarshal(data, &lc)
	return lc
}

func saveLastChoice(lc lastChoice) {
	_ = os.MkdirAll(dataDir(), 0755)
	data, err := json.MarshalIndent(lc, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(lastChoicePath(), data, 0644)
}

// pickProvider prints the available providers and reads a choice from stdin.
// `defaultKey` (if present in `providers`) is selectable by pressing Enter.
// `allowNone` adds an option to skip selection entirely (used for the pruner,
// which is optional). Returns the chosen key, or "" if the user picked none.
func pickProvider(role, defaultKey string, providers map[string]Provider, allowNone bool) string {
	keys := providerNames(providers)
	if len(keys) == 0 {
		if !allowNone {
			fmt.Fprintln(os.Stderr, "no providers configured; create "+providersPath())
			os.Exit(1)
		}
		return ""
	}

	fmt.Printf("\nselect %s:\n", role)
	for i, k := range keys {
		marker := " "
		if k == defaultKey {
			marker = "*"
		}
		fmt.Printf("  %s %d) %s\n", marker, i+1, k)
	}
	if allowNone {
		fmt.Printf("    %d) (none — disable %s)\n", len(keys)+1, role)
	}
	prompt := "choice"
	if defaultKey != "" {
		prompt += " [enter for *]"
	}
	prompt += ": "

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return defaultKey
		}
		choice := strings.TrimSpace(line)
		if choice == "" && defaultKey != "" {
			return defaultKey
		}
		// Allow typing a key directly.
		if _, ok := providers[strings.ToLower(choice)]; ok {
			return strings.ToLower(choice)
		}
		n, err := strconv.Atoi(choice)
		if err != nil {
			fmt.Println("invalid; try again")
			continue
		}
		if n >= 1 && n <= len(keys) {
			return keys[n-1]
		}
		if allowNone && n == len(keys)+1 {
			return ""
		}
		fmt.Println("out of range; try again")
	}
}

// resolveProviderInteractive picks the main provider, falling back to the
// interactive picker when the env-driven resolution can't decide.
func resolveProviderInteractive(providers map[string]Provider, defaultKey string) (Provider, string, error) {
	if p, err := ResolveProvider(providers); err == nil {
		// Env / single-provider path — return whatever it picked. We still
		// record the canonical key when we can.
		key := canonicalKey(providers, p)
		return p, key, nil
	} else if err != ErrAmbiguousProvider {
		return Provider{}, "", err
	}
	key := pickProvider("main agent", defaultKey, providers, false)
	if key == "" {
		return Provider{}, "", fmt.Errorf("no main agent selected")
	}
	p, err := applyProviderEnvOverrides(providers[key])
	return p, key, err
}

// resolvePrunerInteractive picks the pruner provider. Returns ("", nil) if the
// user opted out (pruner disabled).
func resolvePrunerInteractive(providers map[string]Provider, defaultKey string) (Provider, string, error) {
	// Allow LLM_PRUNER_PROVIDER to bypass the picker, mirroring LLM_PROVIDER.
	if sel := strings.TrimSpace(envString("LLM_PRUNER_PROVIDER")); sel != "" {
		if sel == "off" || sel == "none" {
			return Provider{}, "", nil
		}
		if p, ok := providers[strings.ToLower(sel)]; ok {
			pp, err := applyProviderEnvOverrides(p)
			return pp, strings.ToLower(sel), err
		}
		return Provider{}, "", fmt.Errorf("LLM_PRUNER_PROVIDER=%q not found in %s", sel, providersPath())
	}
	key := pickProvider("pruner (cleans context when it grows)", defaultKey, providers, true)
	if key == "" {
		return Provider{}, "", nil
	}
	p, err := applyProviderEnvOverrides(providers[key])
	return p, key, err
}

// canonicalKey returns the providers-map key for a resolved Provider, or
// empty when the provider came from raw env (not in the map).
func canonicalKey(providers map[string]Provider, p Provider) string {
	want := strings.ToLower(p.Name) + "/" + p.Model
	if _, ok := providers[want]; ok {
		return want
	}
	return ""
}
