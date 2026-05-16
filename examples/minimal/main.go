// Minimal axon embed: one Go file, one custom tool, one instruction.
//
// Run:
//
//	OPENAI_API_KEY=sk-... go run ./examples/minimal
//
// This is what "use the runtime" looks like. The reference CLI in
// cmd/axon does more — interactive provider picker, YAML loader, TTY
// renderer, slash commands — but none of that is required to embed.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/atakang7/axon/agent"
)

func main() {
	deploy := agent.Tool{
		Name:        "deploy",
		Description: "Deploy a named service to staging.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"service": map[string]any{"type": "string"},
			},
			"required": []string{"service"},
		},
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct{ Service string }
			if err := json.Unmarshal(args, &p); err != nil {
				return "", err
			}
			return fmt.Sprintf("deployed %s to staging", p.Service), nil
		},
	}

	ag, err := agent.New(agent.Config{
		Provider: agent.Provider{
			Name:    "openai",
			Model:   "gpt-4o",
			BaseURL: "https://api.openai.com",
			APIKey:  os.Getenv("OPENAI_API_KEY"),
		},
		SystemPrompt: "You are a deployment assistant. Use the deploy tool to ship services on request.",
		Tools:        []agent.Tool{deploy},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer ag.Close()

	res, err := ag.Step(context.Background(), "deploy the api service")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Assistant)
}
