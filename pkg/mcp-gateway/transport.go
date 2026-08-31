package mcpgateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"symagios/egress"
)

// Transport kinds supported by the gateway.
const (
	TransportStub  = "stub"
	TransportHTTP  = "http"  // streamable HTTP / JSON-RPC POST to BaseURL
	TransportSSE   = "sse"   // MCP SSE: GET event stream + POST messages
	TransportStdio = "stdio" // local process (jailed by engine when possible)
)

// Transport calls tools/call on an attached MCP server.
type Transport interface {
	Call(ctx context.Context, server ServerSpec, tool ToolSpec, args json.RawMessage, token string) (string, error)
}

func (s *Service) transportFor(server ServerSpec) Transport {
	t := strings.ToLower(strings.TrimSpace(server.Transport))
	if t == "" {
		base := strings.TrimSpace(server.BaseURL)
		switch {
		case base == "" || strings.HasPrefix(base, "stub://"):
			t = TransportStub
		case strings.HasPrefix(base, "stdio://"):
			t = TransportStdio
		default:
			t = TransportHTTP
		}
	}
	switch t {
	case TransportSSE:
		return sseTransport{http: s.http, source: s.cfg.Source}
	case TransportStdio:
		return stdioTransport{}
	case TransportStub:
		return stubTransport{}
	default:
		return httpTransport{http: s.http, source: s.cfg.Source}
	}
}

type stubTransport struct{}

func (stubTransport) Call(_ context.Context, _ ServerSpec, tool ToolSpec, args json.RawMessage, _ string) (string, error) {
	return stubCall(tool.Name, args), nil
}

type httpTransport struct {
	http   *http.Client
	source string
}

func (t httpTransport) Call(ctx context.Context, server ServerSpec, tool ToolSpec, args json.RawMessage, token string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(server.BaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("http transport: base_url required")
	}
	endpoint := base
	if !strings.HasSuffix(base, "/mcp") && !strings.Contains(base, "/messages") {
		endpoint = base + "/mcp"
	}
	return jsonRPCCall(ctx, t.http, endpoint, server, tool, args, token, t.source)
}

type sseTransport struct {
	http   *http.Client
	source string
}

func (t sseTransport) Call(ctx context.Context, server ServerSpec, tool ToolSpec, args json.RawMessage, token string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(server.BaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("sse transport: base_url required")
	}
	// MCP SSE pattern: POST JSON-RPC to /message (or BaseURL) after opening SSE.
	// Many servers accept tools/call on the messages endpoint directly.
	msgURL := base + "/message"
	if server.MessagesURL != "" {
		msgURL = server.MessagesURL
	}
	return jsonRPCCall(ctx, t.http, msgURL, server, tool, args, token, t.source)
}

func jsonRPCCall(ctx context.Context, client *http.Client, url string, server ServerSpec, tool ToolSpec, args json.RawMessage, token, source string) (string, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool.Name,
			"arguments": rawOrEmptyObject(args),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set(server.AuthHeader, server.AuthPrefix+token)
	}
	if err := egress.SetSource(req, source); err != nil {
		return "", fmt.Errorf("mcp %s egress: %w", server.ID, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mcp %s: %w", server.ID, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("mcp %s: %s: %s", server.ID, resp.Status, truncate(string(raw), 200))
	}
	// SSE response: take last data: line that looks like JSON-RPC
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") || bytes.Contains(raw, []byte("data:")) {
		return parseSSEJSONRPC(raw)
	}
	return parseJSONRPCResult(raw)
}

func parseSSEJSONRPC(raw []byte) (string, error) {
	var last string
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data:") {
			last = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if last == "" || last == "[DONE]" {
		return string(raw), nil
	}
	return parseJSONRPCResult([]byte(last))
}

func parseJSONRPCResult(raw []byte) (string, error) {
	var rpc struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &rpc); err != nil {
		return string(raw), nil
	}
	if rpc.Error != nil {
		return "", fmt.Errorf("mcp tool error: %s", rpc.Error.Message)
	}
	if len(rpc.Result) == 0 {
		return string(raw), nil
	}
	return string(rpc.Result), nil
}

// stdioTransport runs a local MCP server over stdin/stdout JSON-RPC lines.
type stdioTransport struct{}

var stdioSeq atomic.Int64

func (stdioTransport) Call(ctx context.Context, server ServerSpec, tool ToolSpec, args json.RawMessage, token string) (string, error) {
	cmdPath := server.Command
	if cmdPath == "" && strings.HasPrefix(server.BaseURL, "stdio://") {
		cmdPath = strings.TrimPrefix(server.BaseURL, "stdio://")
	}
	if cmdPath == "" {
		return "", fmt.Errorf("stdio transport: command required")
	}
	cmd := exec.CommandContext(ctx, cmdPath, server.Args...)
	cmd.Env = append(os.Environ(), server.Env...)
	if token != "" {
		cmd.Env = append(cmd.Env, "MCP_AUTH_TOKEN="+token)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", err
	}

	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		defer close(ch)
		id := stdioSeq.Add(1)
		// initialize then tools/call (minimal MCP handshake)
		for _, msg := range []map[string]any{
			{
				"jsonrpc": "2.0", "id": id, "method": "initialize",
				"params": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"clientInfo":      map[string]string{"name": "symagios-mcp-gateway", "version": "0.1"},
				},
			},
			{"jsonrpc": "2.0", "method": "notifications/initialized"},
			{
				"jsonrpc": "2.0", "id": id + 1, "method": "tools/call",
				"params": map[string]any{"name": tool.Name, "arguments": rawOrEmptyObject(args)},
			},
		} {
			b, _ := json.Marshal(msg)
			if _, err := stdin.Write(append(b, '\n')); err != nil {
				ch <- result{err: err}
				return
			}
		}
		_ = stdin.Close()

		deadline := time.After(25 * time.Second)
		reader := bufio.NewReader(stdout)
		for {
			select {
			case <-ctx.Done():
				ch <- result{err: ctx.Err()}
				return
			case <-deadline:
				ch <- result{err: fmt.Errorf("stdio mcp timeout; stderr=%s", truncate(stderr.String(), 200))}
				return
			default:
			}
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				var rpc struct {
					ID     json.RawMessage `json:"id"`
					Result json.RawMessage `json:"result"`
					Error  *struct {
						Message string `json:"message"`
					} `json:"error"`
					Method string `json:"method"`
				}
				if json.Unmarshal(line, &rpc) == nil {
					if rpc.Error != nil {
						ch <- result{err: fmt.Errorf("mcp: %s", rpc.Error.Message)}
						return
					}
					if len(rpc.Result) > 0 && rpc.Method == "" {
						ch <- result{text: string(rpc.Result)}
						return
					}
				}
			}
			if err == io.EOF {
				ch <- result{err: fmt.Errorf("stdio mcp closed without result; stderr=%s", truncate(stderr.String(), 200))}
				return
			}
			if err != nil {
				ch <- result{err: err}
				return
			}
		}
	}()

	res := <-ch
	_ = cmd.Process.Kill()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = cmd.Wait() }()
	wg.Wait()
	return res.text, res.err
}
