package main

import (
	"bufio"
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ui := UI{}
	registry := NewProviderRegistry()
	if err := LoadConfigFile(registry); err != nil {
		ui.Error(err)
		return
	}

	providerName := SelectedProviderName(os.Getenv)
	client, err := registry.NewClient(providerName)
	if err != nil {
		ui.Error(err)
		return
	}

	session := LoadOrCreateSession()
	provider, _ := registry.Get(providerName)
	ui.Header(provider.Name, provider.Model, session)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	tools := []ToolDefinition{ReadFileDefinition, ListFilesDefinition, EditFileDefinition(session)}
	agent := NewAgent(client, func() (string, bool) {
		if !scanner.Scan() {
			return "", false
		}
		return scanner.Text(), true
	}, tools, session)

	if err := agent.Run(ctx); err != nil {
		ui.Error(err)
	}
}
