package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/atakang7/axon/agent"
)

// lastChoice persists the user's previously selected providers so subsequent
// runs can offer them as defaults. Stored alongside session data, so wiping
// data resets the choices too.
type lastChoice struct {
	Main   string `json:"main,omitempty"`
	Pruner string `json:"pruner,omitempty"`
}

func lastChoicePath() string {
	return filepath.Join(agent.DataDir(), "last_choice.json")
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
	_ = os.MkdirAll(agent.DataDir(), 0755)
	data, err := json.MarshalIndent(lc, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(lastChoicePath(), data, 0644)
}

// pickProvider prints the available providers and reads a choice from stdin.
func pickProvider(role, defaultKey string, providers map[string]agent.Provider, allowNone bool) string {
	keys := agent.ProviderNames(providers)
	if len(keys) == 0 {
		if !allowNone {
			fmt.Fprintln(os.Stderr, "no providers configured; create "+agent.ProvidersPath())
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

func resolveProviderInteractive(providers map[string]agent.Provider, defaultKey string) (agent.Provider, string, error) {
	if p, err := agent.ResolveProvider(providers); err == nil {
		key := canonicalKey(providers, p)
		return p, key, nil
	} else if !errors.Is(err, agent.ErrAmbiguousProvider) {
		return agent.Provider{}, "", err
	}
	key := pickProvider("main agent", defaultKey, providers, false)
	if key == "" {
		return agent.Provider{}, "", fmt.Errorf("no main agent selected")
	}
	p, err := agent.ApplyProviderEnvOverrides(providers[key])
	return p, key, err
}

func resolvePrunerInteractive(providers map[string]agent.Provider, defaultKey string) (agent.Provider, string, error) {
	if sel := strings.TrimSpace(agent.EnvString("LLM_PRUNER_PROVIDER")); sel != "" {
		if sel == "off" || sel == "none" {
			return agent.Provider{}, "", nil
		}
		if p, ok := providers[strings.ToLower(sel)]; ok {
			pp, err := agent.ApplyProviderEnvOverrides(p)
			return pp, strings.ToLower(sel), err
		}
		return agent.Provider{}, "", fmt.Errorf("LLM_PRUNER_PROVIDER=%q not found in %s", sel, agent.ProvidersPath())
	}
	key := pickProvider("pruner (cleans context when it grows)", defaultKey, providers, true)
	if key == "" {
		return agent.Provider{}, "", nil
	}
	p, err := agent.ApplyProviderEnvOverrides(providers[key])
	return p, key, err
}

func canonicalKey(providers map[string]agent.Provider, p agent.Provider) string {
	want := strings.ToLower(p.Name) + "/" + p.Model
	if _, ok := providers[want]; ok {
		return want
	}
	return ""
}
