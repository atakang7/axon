// Command axon is the reference CLI built on the axon agent runtime.
//
// All runtime logic lives in github.com/atakang7/axon/agent. This binary
// wires the runtime to a terminal: an interactive provider picker, a
// YAML loader for agent personalities, a colored TTY renderer, and
// slash commands. It is one consumer of the library, not the product.
//
// Embedders building agents in Go should import the agent package
// directly — this CLI is a reference, not the only shape a consumer
// can take. See examples/minimal for the minimum-viable embed.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/atakang7/axon/agent"
)

func main() {
	var (
		flagPrompt = flag.String("prompt", "", "Run a single prompt non-interactively and exit when the assistant emits a final reply. Requires LLM_PROVIDER env to be set to skip the provider picker.")
		flagAgent  = flag.String("agent", "", "Named agent config to load from $AXON_AGENTS_DIR (default ~/.config/axon/agents/<name>.yaml). Empty = built-in default CLI agent.")
	)
	flag.Parse()

	agentCfg, err := LoadAgentConfig(*flagAgent)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent config:", err)
		os.Exit(1)
	}

	nonInteractive := *flagPrompt != ""
	if nonInteractive {
		uiSilent = true
	}

	providers, err := agent.LoadProviders()
	if err != nil {
		uiError(err)
		return
	}
	lc := loadLastChoice()

	var (
		p       agent.Provider
		mainKey string
	)
	if nonInteractive {
		p, err = agent.ResolveProvider(providers)
		if err != nil {
			fmt.Fprintln(os.Stderr, "non-interactive mode requires LLM_PROVIDER:", err)
			os.Exit(1)
		}
		mainKey = canonicalKey(providers, p)
	} else {
		p, mainKey, err = resolveProviderInteractive(providers, lc.Main)
		if err != nil {
			uiError(err)
			return
		}
	}

	var (
		prunerProvider agent.Provider
		prunerKey      string
		pruner         *agent.Pruner
	)
	if nonInteractive {
		if sel := agent.EnvString("LLM_PRUNER_PROVIDER"); sel != "" && sel != "off" && sel != "none" {
			prunerProvider, prunerKey, err = resolvePrunerInteractive(providers, lc.Pruner)
			if err != nil {
				uiError(err)
				return
			}
		}
	} else {
		prunerProvider, prunerKey, err = resolvePrunerInteractive(providers, lc.Pruner)
		if err != nil {
			uiError(err)
			return
		}
	}
	if prunerKey != "" {
		pc, err := agent.NewClient(prunerProvider)
		if err != nil {
			uiError(err)
			return
		}
		pruner = agent.NewPruner(pc)
	}

	if !nonInteractive {
		saveLastChoice(lastChoice{Main: mainKey, Pruner: prunerKey})
	}

	// Resolve system prompt: YAML wins, otherwise the CLI's default.
	systemPrompt := defaultCLIPrompt
	if agentCfg != nil {
		if body, err := agentCfg.LoadSystemPrompt(); err == nil && strings.TrimSpace(body) != "" {
			systemPrompt = body
		} else if err != nil {
			fmt.Fprintln(os.Stderr, "warning: agent system_prompt:", err)
		}
	}
	customTools, err := agentCfg.BuildTools()
	if err != nil {
		uiError(err)
		return
	}

	tty := newTTYHandler()

	ag, err := agent.New(agent.Config{
		Provider:     p,
		SystemPrompt: systemPrompt,
		Tools:        customTools,
		Pruner:       pruner,
		OnEvent:      tty.Handle,
	})
	if err != nil {
		uiError(err)
		return
	}
	defer ag.Close()

	if !nonInteractive {
		uiHeader(p.Name, p.Model, ag.Session())
		if pruner != nil {
			uiInfo(fmt.Sprintf("pruner: %s/%s", prunerProvider.Name, prunerProvider.Model))
		} else {
			uiInfo("pruner: disabled")
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	var inputFn func() (string, bool)
	if nonInteractive {
		inputFn = singleShotInput(*flagPrompt)
	} else {
		inputFn = pasteAwareInput(os.Stdin)
	}

	if !nonInteractive {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		go func() {
			for range sigint {
				if ag.Interrupt() {
					continue
				}
				_ = ag.Close()
				os.Exit(130)
			}
		}()
	}

	// REPL: read input, handle slash commands, otherwise drive a Step.
	for {
		uiPrompt()
		line, ok := inputFn()
		if !ok {
			break
		}
		uiAfterInput()
		trimmed := strings.TrimSpace(line)
		if handleSlash(ag, trimmed) {
			continue
		}
		if _, err := ag.Step(ctx, line); err != nil {
			uiError(err)
		}
	}
}

// defaultCLIPrompt is the role text the reference CLI uses when no
// --agent personality is supplied. The runtime itself has no default
// prompt; if you're building a different product on top of the agent
// package you should provide your own.
const defaultCLIPrompt = `You are a helpful AI assistant with file-system and shell tools. Read, write, search, and execute commands to accomplish what the user asks. Be concise; act rather than narrate.`
