---
title: Getting Started
description: Bootstrap the Axon runtime.
---

Integrating Axon requires setting up your configuration, allocating a provider client, and initializing the `Agent` struct.

## Installation

```bash
go get github.com/atakang7/axon/v2
```

## The Minimal Implementation

The following demonstrates the canonical initialization sequence for an Axon agent. 

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/atakang7/axon/v2"
)

func main() {
	// 1. Load configuration (Settings + Secrets).
	settings, err := axon.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// 2. Allocate the provider client.
	model, err := settings.NewClient("openrouter", "deepseek/deepseek-v3.2")
	if err != nil {
		log.Fatalf("Failed to initialize model client: %v", err)
	}

	// 3. Construct the Agent runtime.
	ag, err := axon.New(axon.Config{
		Model:        model,
		SystemPrompt: "You are an expert systems engineer. Use tools efficiently.",
		Settings:     settings,
	})
	if err != nil {
		log.Fatalf("Failed to construct agent: %v", err)
	}
	defer ag.Close()

	// 4. Dispatch a turn.
	ctx := context.Background()
	response, err := ag.Step(ctx, "Analyze the system environment and list active processes.")
	if err != nil {
		log.Fatalf("Agent step failed: %v", err)
	}

	fmt.Println("Final Response:", response)
}
```

By default, `axon.New` attaches a standard library of tools (read, write, exec, search, task) allowing the agent to interact with the host environment immediately.
