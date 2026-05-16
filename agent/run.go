package agent

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// Main is the CLI entry point. It parses flags, resolves providers, builds the
// agent, and runs the REPL (or single-shot turn for --prompt). cmd/axon/main.go
// is a thin wrapper that calls this.
func Main() {
	var (
		flagPrompt  = flag.String("prompt", "", "Run a single prompt non-interactively and exit when the assistant emits a final reply (no tool calls). Requires LLM_PROVIDER env to be set to skip the provider picker.")
		flagLogJSON = flag.String("log-json", "", "Write a JSONL event log to this path (events: prompt, assistant_text, tool_call, tool_result, error, done).")
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

	var logger *jsonlLogger
	if *flagLogJSON != "" {
		l, err := newJSONLLogger(*flagLogJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, "log-json:", err)
			os.Exit(1)
		}
		logger = l
		defer logger.Close()
	}

	providers, err := LoadProviders()
	if err != nil {
		uiError(err)
		logger.Emit("error", map[string]any{"where": "load_providers", "error": err.Error()})
		return
	}
	lc := loadLastChoice()

	var (
		p       Provider
		mainKey string
	)
	if nonInteractive {
		p, err = ResolveProvider(providers)
		if err != nil {
			fmt.Fprintln(os.Stderr, "non-interactive mode requires LLM_PROVIDER:", err)
			logger.Emit("error", map[string]any{"where": "resolve_provider", "error": err.Error()})
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
	client, err := NewClient(p)
	if err != nil {
		uiError(err)
		logger.Emit("error", map[string]any{"where": "new_client", "error": err.Error()})
		return
	}

	var (
		prunerProvider Provider
		prunerKey      string
		pruner         *Pruner
	)
	if nonInteractive {
		// Default: pruner off in non-interactive mode unless explicitly enabled.
		if sel := envString("LLM_PRUNER_PROVIDER"); sel != "" && sel != "off" && sel != "none" {
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
		pc, err := NewClient(prunerProvider)
		if err != nil {
			uiError(err)
			return
		}
		pruner = NewPruner(pc)
	}

	if !nonInteractive {
		saveLastChoice(lastChoice{Main: mainKey, Pruner: prunerKey})
	}

	session := LoadOrCreateSession()
	if !nonInteractive {
		uiHeader(p.Name, p.Model, session)
		if pruner != nil {
			uiInfo(fmt.Sprintf("pruner: %s/%s", prunerProvider.Name, prunerProvider.Model))
		} else {
			uiInfo("pruner: disabled")
		}
	}

	logger.Emit("session_start", map[string]any{
		"provider":  p.Name,
		"model":     p.Model,
		"cwd":       session.Cwd,
		"session":   sessionPath(),
		"turn":      session.Turn,
		"pruner_on": pruner != nil,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	tools, err := BuildTools(session, agentCfg)
	if err != nil {
		uiError(err)
		logger.Emit("error", map[string]any{"where": "build_tools", "error": err.Error()})
		return
	}
	var inputFn func() (string, bool)
	if nonInteractive {
		inputFn = singleShotInput(*flagPrompt)
		logger.Emit("prompt", map[string]any{"text": *flagPrompt})
	} else {
		inputFn = pasteAwareInput(os.Stdin)
	}
	agent := &Agent{client: client, tools: tools, session: session,
		input:  inputFn,
		pruner: pruner,
		logger: logger,
		cfg:    agentCfg,
	}

	if !nonInteractive {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		go func() {
			for range sigint {
				if agent.InterruptTurn() {
					continue
				}
				bgReg.killAll()
				os.Exit(130)
			}
		}()
	}

	defer bgReg.killAll()

	runErr := agent.Run(ctx)
	if runErr != nil {
		uiError(runErr)
		logger.Emit("error", map[string]any{"where": "run", "error": runErr.Error()})
	}
	logger.Emit("done", map[string]any{"turns": session.Turn})
}
