//go:build api

package main

// api.go — HTTP API server for axon. Same agent/session/tool logic as the
// CLI, exposed over HTTP with SSE streaming for turn responses.
//
// Build:  go build -tags api -o axon-api .
// Run:    AXON_API_ADDR=:8080 ./axon-api
//
// Sessions are stored on disk (same mechanism as the CLI). Each session maps
// to one JSON file under $AXON_DATA_DIR/sessions/<id>.json.
//
// Endpoints:
//   GET    /sessions              list all sessions
//   POST   /sessions              create session (body: {"provider":"key"})
//   GET    /sessions/:id          get session state
//   DELETE /sessions/:id          delete session
//   POST   /sessions/:id/turns    send a message, stream response via SSE
//   POST   /sessions/:id/model    switch provider/model
//   POST   /sessions/:id/reset    clear messages, keep session id

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ── stub out CLI-only functions so shared files compile under -tags api ──────

func uiError(_ error)                         {}
func uiInfo(_ string)                         {}
func uiPrompt()                               {}
func uiAfterInput()                           {}
func uiStartResponse()                        {}
func uiToken(_ string)                        {}
func uiResponse()                             {}
func uiReasoning(_ string)                    {}
func uiToolArgStart(_ string)                 {}
func uiToolArgDelta(_, _ string)              {}
func uiToolArgEnd()                           {}
func uiTool(_ string, _ []byte)               {}
func uiToolResult(_ string)                   {}
func uiToolError(_ error)                     {}
func uiPhase(_ string, _ time.Duration)       {}
func uiSpinner() func()                       { return func() {} }
func uiUndone(_ string)                       {}
func uiMemory(_ string)                       {}
func uiSessionNew()                           {}
func uiSessionInfo(_ *Session)                {}
func uiHelp()                                 {}
func uiHeader(_ string, _ string, _ *Session) {}
func uiReplayHistory(_ *Session)              {}

// SlashCommand / SlashCommands are referenced in agent.go handleSlash.
type SlashCommand struct{ Name, Args, Help string }

var SlashCommands []SlashCommand

func uiProviderPicker(_ map[string]Provider, _ func() (string, bool)) (string, error) {
	return "", fmt.Errorf("not available in API mode")
}
func uiModelPicker(_ map[string]Provider, _ func() (string, bool)) (string, error) {
	return "", fmt.Errorf("not available in API mode")
}
func uiResolveModelArg(providers map[string]Provider, arg string) string {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if _, ok := providers[arg]; ok {
		return arg
	}
	var matches []string
	for k, p := range providers {
		if strings.Contains(strings.ToLower(p.Model), arg) || strings.Contains(strings.ToLower(k), arg) {
			matches = append(matches, k)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

// ── session store ─────────────────────────────────────────────────────────────

// apiSessionPath returns the path for a session file given its ID.
func apiSessionPath(id string) string {
	return filepath.Join(dataDir(), "sessions", id+".json")
}

func apiLoadSession(id string) (*Session, error) {
	p := apiSessionPath(id)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	s.path = p
	s.ensure()
	return &s, nil
}

func apiCreateSession(providerKey string, providers map[string]Provider) (*Session, Provider, error) {
	p, ok := providers[providerKey]
	if !ok {
		return nil, Provider{}, fmt.Errorf("provider %q not found", providerKey)
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	s := &Session{
		ID:        id,
		StartedAt: time.Now(),
		path:      apiSessionPath(id),
	}
	s.ensure()
	initSessionMessages(s)
	if err := s.Save(); err != nil {
		return nil, Provider{}, err
	}
	return s, p, nil
}

func apiListSessions() ([]map[string]any, error) {
	dir := filepath.Join(dataDir(), "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []map[string]any{}, nil
		}
		return nil, err
	}
	var out []map[string]any
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id":         s.ID,
			"started_at": s.StartedAt,
			"turn":       s.Turn,
			"messages":   len(s.Messages),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprint(out[i]["started_at"]) > fmt.Sprint(out[j]["started_at"])
	})
	return out, nil
}

// ── SSE helpers ───────────────────────────────────────────────────────────────

type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSE(w http.ResponseWriter) (*sseWriter, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	return &sseWriter{w, f}, true
}

func (s *sseWriter) send(v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

// ── turn runner ───────────────────────────────────────────────────────────────

// runTurn executes one agent turn (may involve multiple LLM calls + tool
// calls) and streams every event to sse. It reuses the Agent.chat and
// Agent.runTool logic verbatim — the only difference is callbacks write to
// SSE instead of the terminal.
func runTurn(ctx context.Context, session *Session, userMsg string, provider Provider, providers map[string]Provider, sse *sseWriter) error {
	session.Turn++
	session.Append(Msg{Role: "user", Content: userMsg})
	session.Save()

	tools := BuildTools(session)

	// mu guards writes to sse since callbacks fire from goroutines.
	var mu sync.Mutex
	send := func(v any) {
		mu.Lock()
		defer mu.Unlock()
		sse.send(v)
	}

	client, err := NewClient(provider)
	if err != nil {
		return err
	}

	agent := &Agent{
		client:  client,
		tools:   tools,
		session: session,
		input:   func() (string, bool) { return "", false },
	}
	_ = providers
	_ = provider

	for {
		toolArgNames := map[string]bool{}
		msg, err := client.ChatStream(ctx,
			session.ContextMessages(), tools,
			func(token string) {
				send(map[string]any{"type": "token", "content": token})
			},
			func(token string) {
				send(map[string]any{"type": "reasoning", "content": token})
			},
			func(name, delta string) {
				if !toolArgNames[name] {
					toolArgNames[name] = true
					send(map[string]any{"type": "tool_start", "name": name})
				}
				send(map[string]any{"type": "tool_arg", "name": name, "delta": delta})
			},
			nil,
			func(phase string, dt time.Duration) {
				send(map[string]any{"type": "phase", "name": phase, "ms": dt.Milliseconds()})
			},
		)
		if err != nil {
			return err
		}

		session.Append(*msg)
		session.Save()

		if len(msg.ToolCalls) == 0 {
			break
		}

		for _, tc := range msg.ToolCalls {
			send(map[string]any{"type": "tool_call", "name": tc.Function.Name, "args": tc.Function.Arguments})
			result := agent.runTool(ctx, tc)
			session.Append(result)
			if id := session.Messages[len(session.Messages)-1].ID; id != "" {
				session.Messages[len(session.Messages)-1].Content = "[#" + id + "]\n" + result.Content
			}
			session.Save()
			send(map[string]any{"type": "tool_result", "name": tc.Function.Name, "content": result.Content})
		}
	}
	send(map[string]any{"type": "done", "turn": session.Turn})
	return nil
}

// ── router ────────────────────────────────────────────────────────────────────

func apiRouter(providers map[string]Provider) http.Handler {
	mux := http.NewServeMux()

	// CORS preflight
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	})

	json200 := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(v)
	}

	jsonErr := func(w http.ResponseWriter, code int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}

	// GET /providers
	mux.HandleFunc("GET /providers", func(w http.ResponseWriter, r *http.Request) {
		type row struct {
			Key   string `json:"key"`
			Name  string `json:"name"`
			Model string `json:"model"`
		}
		keys := make([]string, 0, len(providers))
		for k := range providers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rows := make([]row, len(keys))
		for i, k := range keys {
			p := providers[k]
			rows[i] = row{k, p.Name, p.Model}
		}
		json200(w, rows)
	})

	// GET /sessions
	mux.HandleFunc("GET /sessions", func(w http.ResponseWriter, r *http.Request) {
		list, err := apiListSessions()
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		json200(w, list)
	})

	// POST /sessions
	mux.HandleFunc("POST /sessions", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Provider string `json:"provider"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Provider == "" {
			// default: first provider alphabetically
			keys := make([]string, 0, len(providers))
			for k := range providers {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if len(keys) == 0 {
				jsonErr(w, 400, "no providers configured")
				return
			}
			body.Provider = keys[0]
		}
		s, p, err := apiCreateSession(body.Provider, providers)
		if err != nil {
			jsonErr(w, 400, err.Error())
			return
		}
		w.WriteHeader(http.StatusCreated)
		json200(w, map[string]any{"id": s.ID, "provider": p.Name, "model": p.Model})
	})

	// GET /sessions/:id
	mux.HandleFunc("GET /sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		s, err := apiLoadSession(r.PathValue("id"))
		if err != nil {
			jsonErr(w, 404, "session not found")
			return
		}
		json200(w, map[string]any{
			"id":            s.ID,
			"started_at":    s.StartedAt,
			"turn":          s.Turn,
			"cwd":           s.Cwd,
			"messages":      s.Messages,
			"parked_blocks": s.ParkedBlocks,
		})
	})

	// DELETE /sessions/:id
	mux.HandleFunc("DELETE /sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		p := apiSessionPath(r.PathValue("id"))
		if err := os.Remove(p); err != nil {
			jsonErr(w, 404, "session not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// POST /sessions/:id/reset
	mux.HandleFunc("POST /sessions/{id}/reset", func(w http.ResponseWriter, r *http.Request) {
		s, err := apiLoadSession(r.PathValue("id"))
		if err != nil {
			jsonErr(w, 404, "session not found")
			return
		}
		s.Messages = nil
		s.ParkedBlocks = nil
		s.Turn = 0
		s.Edits = nil
		s.ensure()
		initSessionMessages(s)
		s.Save()
		json200(w, map[string]any{"id": s.ID, "turn": s.Turn})
	})

	// POST /sessions/:id/model
	mux.HandleFunc("POST /sessions/{id}/model", func(w http.ResponseWriter, r *http.Request) {
		s, err := apiLoadSession(r.PathValue("id"))
		if err != nil {
			jsonErr(w, 404, "session not found")
			return
		}
		var body struct {
			Provider string `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Provider == "" {
			jsonErr(w, 400, "body must be {\"provider\":\"<key>\"}")
			return
		}
		p, ok := providers[body.Provider]
		if !ok {
			jsonErr(w, 400, fmt.Sprintf("provider %q not found", body.Provider))
			return
		}
		_ = s
		json200(w, map[string]any{"provider": p.Name, "model": p.Model})
	})

	// POST /sessions/:id/turns  — SSE streaming
	mux.HandleFunc("POST /sessions/{id}/turns", func(w http.ResponseWriter, r *http.Request) {
		s, err := apiLoadSession(r.PathValue("id"))
		if err != nil {
			jsonErr(w, 404, "session not found")
			return
		}
		var body struct {
			Message  string `json:"message"`
			Provider string `json:"provider"` // optional override
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Message) == "" {
			jsonErr(w, 400, "body must be {\"message\":\"...\"}")
			return
		}

		// Resolve provider: per-request override → session default → first.
		providerKey := body.Provider
		if providerKey == "" {
			// Try to read the key stored on the session (we piggyback on Cwd
			// comment field — actually store in a dedicated field below). For
			// now fall back to first provider.
			keys := make([]string, 0, len(providers))
			for k := range providers {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if len(keys) == 0 {
				jsonErr(w, 500, "no providers configured")
				return
			}
			providerKey = keys[0]
		}
		provider, ok := providers[providerKey]
		if !ok {
			jsonErr(w, 400, fmt.Sprintf("provider %q not found", providerKey))
			return
		}

		sse, ok := newSSE(w)
		if !ok {
			jsonErr(w, 500, "streaming not supported")
			return
		}

		if err := runTurn(r.Context(), s, body.Message, provider, providers, sse); err != nil {
			sse.send(map[string]any{"type": "error", "message": err.Error()})
		}
	})

	return mux
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	providers, err := LoadProviders()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading providers: %v\n", err)
		os.Exit(1)
	}

	addr := os.Getenv("AXON_API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: apiRouter(providers),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	go func() {
		fmt.Printf("axon api  %s\n", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
}
