package main

import (
	"bufio"
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	providers, err := LoadProviders()
	if err != nil {
		uiError(err)
		return
	}
	p, err := ResolveProvider(providers)
	if err != nil {
		uiError(err)
		return
	}
	client, err := NewClient(p)
	if err != nil {
		uiError(err)
		return
	}
	session := LoadOrCreateSession()
	uiHeader(p.Name, p.Model, session)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	tools := BuildTools(session)
	agent := &Agent{client: client, tools: tools, session: session,
		input: func() (string, bool) {
			if !scanner.Scan() {
				return "", false
			}
			return scanner.Text(), true
		},
	}
	if err := agent.Run(ctx); err != nil {
		uiError(err)
	}
}
