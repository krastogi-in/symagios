package mcpgateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	mcpgateway "symagios/mcp-gateway"
)

func TestLoadRegistryAndHTTPTransport(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"content": []any{map[string]string{"type": "text", "text": "from-http"}}},
		})
	}))
	defer upstream.Close()

	path := filepath.Join(t.TempDir(), "reg.json")
	regJSON := `{
  "servers": [{
    "id": "u",
    "enabled": true,
    "transport": "http",
    "base_url": "` + upstream.URL + `",
    "tools": [{"name": "ping"}]
  }]
}`
	if err := os.WriteFile(path, []byte(regJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := mcpgateway.LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := mcpgateway.New(mcpgateway.Config{ContentGuards: false}, reg)
	resp, errBody := svc.Call(context.Background(), mcpgateway.CallRequest{Tool: "ping", Arguments: json.RawMessage(`{}`)})
	if errBody != nil {
		t.Fatalf("%#v", errBody)
	}
	if resp.Result == "" {
		t.Fatal("empty result")
	}
}

func TestSourceAllowlist(t *testing.T) {
	reg := mcpgateway.NewRegistry()
	_ = reg.Attach(mcpgateway.ServerSpec{
		ID:             "demo",
		Enabled:        true,
		Transport:      "stub",
		BaseURL:        "stub://",
		AllowedSources: []string{"symagios-engine"},
		Tools:          []mcpgateway.ToolSpec{{Name: "echo"}},
	})
	svc := mcpgateway.New(mcpgateway.Config{ContentGuards: false}, reg)
	_, errBody := svc.Call(context.Background(), mcpgateway.CallRequest{Tool: "echo", Source: "evil"})
	if errBody == nil || errBody.Code != mcpgateway.CodeToolNotAllowed {
		t.Fatalf("%#v", errBody)
	}
	resp, errBody := svc.Call(context.Background(), mcpgateway.CallRequest{Tool: "echo", Source: "symagios-engine"})
	if errBody != nil || resp == nil {
		t.Fatalf("%#v %v", errBody, resp)
	}
}

func TestSSEParseViaTransport(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n"))
	}))
	defer upstream.Close()

	reg := mcpgateway.NewRegistry()
	_ = reg.Attach(mcpgateway.ServerSpec{
		ID:          "sse",
		Enabled:     true,
		Transport:   "sse",
		BaseURL:     upstream.URL,
		MessagesURL: upstream.URL,
		Tools:       []mcpgateway.ToolSpec{{Name: "sse_tool"}},
	})
	svc := mcpgateway.New(mcpgateway.Config{ContentGuards: false}, reg)
	resp, errBody := svc.Call(context.Background(), mcpgateway.CallRequest{Tool: "sse_tool"})
	if errBody != nil {
		t.Fatalf("%#v", errBody)
	}
	if resp.Result == "" {
		t.Fatal("empty")
	}
}
