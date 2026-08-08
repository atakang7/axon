package axon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// MCPServer configures an out-of-process Model Context Protocol server.
// The runtime will spawn it, perform the handshake, discover its tools,
// and kill the process when the agent closes.
type MCPServer struct {
	Command string
	Args    []string
	Env     []string // Optional KEY=VALUE environment variables
}

// mcpClient manages a single MCP subprocess and its JSON-RPC lifecycle.
type mcpClient struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	seq     int64
	pending map[int64]chan []byte
	mu      sync.Mutex // Protects write to stdin
	pendMu  sync.Mutex // Protects pending map
}

type mcpRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// startMCP spawns the server, completes the handshake, and returns the discovered tools.
func startMCP(cfg MCPServer) (*mcpClient, []Tool, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = append(cmd.Environ(), cfg.Env...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("mcp stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("mcp stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("mcp start %q: %w", cfg.Command, err)
	}

	client := &mcpClient{
		cmd:     cmd,
		stdin:   stdin,
		pending: make(map[int64]chan []byte),
	}

	// Read loop
	go func() {
		sc := bufio.NewScanner(stdout)
		// MCP payloads can be large (e.g. schemas). Expand buffer up to 10MB.
		sc.Buffer(make([]byte, 64*1024), 10*1024*1024)
		for sc.Scan() {
			var resp mcpResponse
			line := sc.Bytes()
			if json.Unmarshal(line, &resp) == nil && resp.ID != 0 {
				client.pendMu.Lock()
				ch, ok := client.pending[resp.ID]
				if ok {
					delete(client.pending, resp.ID)
				}
				client.pendMu.Unlock()
				if ok {
					msg := make([]byte, len(line))
					copy(msg, line)
					ch <- msg
				}
			}
		}
	}()

	// 1. Initialize Handshake
	initParams := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "axon",
			"version": "1.0.0",
		},
	}
	if _, err := client.call("initialize", initParams); err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("mcp initialize: %w", err)
	}

	// 2. Initialized Notification
	client.notify("notifications/initialized", nil)

	// 3. List Tools
	rawTools, err := client.call("tools/list", nil)
	if err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("mcp tools/list: %w", err)
	}

	var toolsResp struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rawTools, &toolsResp); err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("mcp parse tools/list: %w", err)
	}

	// 4. Map to Axon Tools
	var axonTools []Tool
	for _, mt := range toolsResp.Tools {
		// Capture by value for closure
		toolName := mt.Name
		axonTools = append(axonTools, Tool{
			Name:        toolName,
			Description: mt.Description,
			Schema:      mt.InputSchema,
			Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
				// We must decode args into map[string]any because MCP tools/call
				// expects the params.arguments object.
				var argMap map[string]any
				if len(args) > 0 {
					if err := json.Unmarshal(args, &argMap); err != nil {
						return "", fmt.Errorf("invalid json args: %w", err)
					}
				}

				callParams := map[string]any{
					"name":      toolName,
					"arguments": argMap,
				}

				resRaw, err := client.call("tools/call", callParams)
				if err != nil {
					return "", err
				}

				var callResp struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
					IsError bool `json:"isError"`
				}
				if err := json.Unmarshal(resRaw, &callResp); err != nil {
					return "", fmt.Errorf("invalid mcp result: %w", err)
				}

				var b strings.Builder
				for _, c := range callResp.Content {
					if c.Type == "text" {
						b.WriteString(c.Text)
					}
				}

				if callResp.IsError {
					return "", fmt.Errorf("mcp tool error: %s", b.String())
				}
				return b.String(), nil
			},
		})
	}

	return client, axonTools, nil
}

// call sends a JSON-RPC request and waits for the response.
func (m *mcpClient) call(method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt64(&m.seq, 1)
	req := mcpRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}

	ch := make(chan []byte, 1)
	m.pendMu.Lock()
	m.pending[id] = ch
	m.pendMu.Unlock()

	if err := m.send(req); err != nil {
		m.pendMu.Lock()
		delete(m.pending, id)
		m.pendMu.Unlock()
		return nil, err
	}

	respBytes := <-ch
	var resp mcpResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s (code: %d)", resp.Error.Message, resp.Error.Code)
	}
	return resp.Result, nil
}

// notify sends a JSON-RPC notification (no response expected).
func (m *mcpClient) notify(method string, params any) error {
	req := mcpRequest{JSONRPC: "2.0", Method: method, Params: params}
	return m.send(req)
}

func (m *mcpClient) send(req any) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	b = append(b, '\n')

	m.mu.Lock()
	defer m.mu.Unlock()
	_, err = m.stdin.Write(b)
	return err
}

// Close kills the background process.
func (m *mcpClient) Close() error {
	if m.cmd != nil && m.cmd.Process != nil {
		m.cmd.Process.Kill()
	}
	return nil
}
