package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"symagios/llm"
)

func TestClaudeHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			http.Error(w, "no key", 401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "claude-test",
			"content": []map[string]string{
				{"type": "text", "text": "hello from claude"},
			},
		})
	}))
	defer srv.Close()

	c, err := llm.New(context.Background(), llm.Config{
		Provider:  llm.ProviderClaude,
		APIKey:    "test",
		BaseURL:   srv.URL,
		Model:     "claude-test",
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello from claude" {
		t.Fatalf("%q", resp.Text)
	}
}

func TestOpenCodexHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "codex-test",
			"choices": []map[string]any{
				{"message": map[string]string{"content": "hello from opencodex"}},
			},
		})
	}))
	defer srv.Close()

	c, err := llm.New(context.Background(), llm.Config{
		Provider:  llm.ProviderOpenCodex,
		APIKey:    "test",
		BaseURL:   srv.URL,
		Model:     "codex-test",
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Complete(context.Background(), llm.Request{
		System:   "sys",
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello from opencodex" {
		t.Fatalf("%q", resp.Text)
	}
}

func TestClaudeViaEgressProxy(t *testing.T) {
	// Upstream (Anthropic stand-in). Proxy routing is on the http.Client;
	// SetSource only stamps X-Symagios-Source when callers use it.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "claude-test",
			"content": []map[string]string{
				{"type": "text", "text": "via egress"},
			},
		})
	}))
	defer upstream.Close()

	c, err := llm.New(context.Background(), llm.Config{
		Provider:  llm.ProviderClaude,
		APIKey:    "test",
		BaseURL:   upstream.URL,
		Model:     "claude-test",
		MaxTokens: 100,
		Source:    "symagios-engine",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "via egress" {
		t.Fatalf("%q", resp.Text)
	}
}
