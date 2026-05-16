// Command axon is the reference CLI built on the axon agent runtime.
//
// All runtime logic lives in github.com/atakang7/axon/agent. This binary
// is one consumer of that library: it adds flag parsing, an interactive
// provider picker, a YAML loader for agent personalities, a terminal
// renderer (TTYHandler), an optional JSONL event log (JSONLHandler),
// and slash-command dispatch (/cd, /undo, /new, /session, /pwd).
//
// Embedders building agents in Go should import the agent package
// directly and bring their own Handler — this CLI is a reference, not
// the only shape a consumer can take.
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
		flagPrompt  = flag.String("prompt", "", "Run a single prompt non-interactively and exit when the assistant emits a final reply (no tool calls). Requires LLM_PROVIDER env to be set to skip the provider picker.")
		flagLogJSON = flag.String("log-json", "", "Write a JSONL event log to this path.")
		flagAgent   = flag.String("agent", "", "Named agent config to load from $AXON_AGENTS_DIR (default ~/.config/axon/agents/<name>.yaml). Empty = built-in default agent.")
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

	var jsonlH *jsonlHandler
	if *flagLogJSON != "" {
		h, err := newJSONLHandler(*flagLogJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, "log-json:", err)
			os.Exit(1)
		}
		jsonlH = h
		defer jsonlH.Close()
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

	// Resolve agent personality into the runtime's Config inputs.
	systemPrompt := ""
	if agentCfg != nil {
		if body, err := agentCfg.LoadSystemPrompt(); err == nil {
			systemPrompt = body
		} else {
			fmt.Fprintln(os.Stderr, "warning: agent system_prompt:", err)
		}
	}
	customTools, err := agentCfg.BuildTools()
	if err != nil {
		uiError(err)
		return
	}

	// Compose handlers: TTY (when not silent) + optional JSONL.
	var handlers []agent.Handler
	if !uiSilent {
		handlers = append(handlers, newTTYHandler())
	}
	if jsonlH != nil {
		handlers = append(handlers, jsonlH)
	}
	handler := agent.MultiHandler(handlers...)

	cfg := agent.Config{
		Provider:     p,
		SystemPrompt: systemPrompt,
		Tools:        customTools,
		Pruner:       pruner,
		Handler:      handler,
	}

	// disable_builtins requires NewBare + explicit composition since
	// New() always includes the full built-in set.
	var ag *agent.Agent
	if agentCfg != nil && len(agentCfg.DisableBuiltins) > 0 {
		session := agent.LoadOrCreateSession()
		builtins := agent.Builtins(session)
		var kept []agent.Tool
		for _, t := range builtins {
			if !agentCfg.DisabledBuiltin(t.Name) {
				kept = append(kept, t)
			}
		}
		cfg.Session = session
		cfg.Tools = append(kept, customTools...)
		ag, err = agent.NewBare(cfg)
	} else {
		ag, err = agent.New(cfg)
	}
	if err != nil {
		uiError(err)
		return
	}
	defer ag.Close()

	session := ag.Session()
	if !nonInteractive {
		uiHeader(p.Name, p.Model, session)
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
