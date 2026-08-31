package llm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"symagios/llm"
	searchgateway "symagios/search-gateway"
)

func TestClaudeSendsOnlyClientSearchTool(t *testing.T) {
	var round atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := round.Add(1)
		body, _ := io.ReadAll(r.Body)
		raw := string(body)
		if strings.Contains(raw, "web_search_20250305") || strings.Contains(raw, "web_search_20260209") || strings.Contains(raw, `"type":"web_search"`) {
			t.Fatalf("provider server search tool was sent: %s", raw)
		}
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}
		tools, _ := req["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("tools=%v", tools)
		}
		tool, _ := tools[0].(map[string]any)
		if tool["name"] != searchgateway.ToolName {
			t.Fatalf("name=%v", tool["name"])
		}
		if _, ok := tool["type"]; ok {
			t.Fatalf("client tool must not use a server type: %v", tool["type"])
		}
		if n == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "claude-test",
				"content": []any{
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_1",
						"name":  searchgateway.ToolName,
						"input": map[string]string{"query": "symagios"},
					},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "claude-test",
			"content": []any{map[string]any{"type": "text", "text": "from tavily"}},
		})
	}))
	defer srv.Close()

	c, err := llm.New(context.Background(), llm.Config{
		Provider: llm.ProviderClaude, APIKey: "k", BaseURL: srv.URL, Model: "claude-test", MaxTokens: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	var q string
	resp, err := c.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "search"}},
		GovernedSearch: func(_ context.Context, query string) (string, error) {
			q = query
			return "hit", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if q != "symagios" || !strings.Contains(resp.Text, "tavily") {
		t.Fatalf("q=%q text=%q", q, resp.Text)
	}
}
