package engine_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"symagios/engine"
)

func TestParseNextActionWebSearch(t *testing.T) {
	act, ok := engine.ParseNextAction(`WEB_SEARCH::web_search{"query":"symagios egress"}`)
	if !ok || act.Kind != engine.NextWebSearch || act.Name != "web_search" {
		t.Fatalf("%+v ok=%v", act, ok)
	}
	if !strings.Contains(act.Payload, "symagios egress") {
		t.Fatalf("payload=%q", act.Payload)
	}
}

func TestCallSearchBrokerOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method %s", r.Method)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["query"] != "hello" {
			t.Fatalf("query=%v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query":  "hello",
			"answer": "world",
			"text":   "world",
			"results": []any{},
		})
	}))
	defer srv.Close()
	t.Setenv("SYMAGIOS_SEARCH_BROKER", srv.URL)

	out := engine.CallSearchBroker(context.Background(), "hello")
	if out.Unavailable || out.Text != "world" {
		t.Fatalf("%+v", out)
	}
}

func TestCallSearchBrokerUnavailable(t *testing.T) {
	t.Setenv("SYMAGIOS_SEARCH_BROKER", "http://127.0.0.1:1/v1/search")
	out := engine.CallSearchBroker(context.Background(), "x")
	if !out.Unavailable {
		t.Fatalf("%+v", out)
	}
	if !strings.Contains(out.Message, "search not available") {
		t.Fatalf("message=%q", out.Message)
	}
}
