package engine_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"symagios/encrypt"
	"symagios/engine"
	"symagios/llm"
	"symagios/storage"
	"symagios/storage/sqlite"
	"symagios/utils/types"
)

func TestSubmitHumanInput(t *testing.T) {
	db := openTestDB(t)
	engine.InitStore(db, nil)

	ctx := context.Background()
	if err := db.InsertActionRun(ctx, storage.ActionRun{
		SessionID:  "s1",
		ActionName: "ask",
		ActionType: types.Agent,
		Status:     types.WaitingForHumanInput,
		Payload:    "q?",
	}); err != nil {
		t.Fatal(err)
	}
	err := engine.SubmitHumanInput(ctx, engine.HumanInputResponse{
		SessionID:  "s1",
		ActionName: "ask",
		ActionType: types.Agent,
		Input:      "answer",
	})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := db.ListActionRunsByStatus(ctx, types.Pending)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("%+v", runs)
	}
	if runs[0].Result != "answer" {
		t.Fatalf("result=%q (action outcome stays in result until fetch)", runs[0].Result)
	}
	if runs[0].Payload != "q?" {
		t.Fatalf("payload should be unchanged until fetch, got %q", runs[0].Payload)
	}
	folded, cleared, changed, err := encrypt.FoldResultIntoPayload(nil, runs[0].Payload, runs[0].Result)
	if err != nil || !changed {
		t.Fatalf("fold: changed=%v err=%v", changed, err)
	}
	if folded != "q?\n\nanswer" || cleared != "" {
		t.Fatalf("after fetch payload=%q result=%q", folded, cleared)
	}
}

func TestCallMCPToolMissingNeedsHuman(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/mcp/tools/call", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "tool_not_found", "message": "missing", "need_human": true,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("SYMAGIOS_MCP_GATEWAY", srv.URL)

	out, err := engine.CallMCPTool(context.Background(), "nope", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !out.NeedHuman || out.Code != "tool_not_found" {
		t.Fatalf("%+v", out)
	}
}

func TestCallMCPToolOK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/mcp/tools/call", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tool": "echo", "server": "demo", "result": `{"ok":true}`,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("SYMAGIOS_MCP_GATEWAY", srv.URL)

	out, err := engine.CallMCPTool(context.Background(), "echo", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if out.NeedHuman || out.Result == "" || out.Server != "demo" {
		t.Fatalf("%+v", out)
	}
}

type mockLLM struct{}

func (mockLLM) Provider() llm.Provider { return llm.ProviderClaude }
func (mockLLM) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	content := ""
	if len(req.Messages) > 0 {
		content = req.Messages[0].Content
	}
	return llm.Response{Text: "mocked:" + content, Model: "mock"}, nil
}

func TestCompleteLLM(t *testing.T) {
	engine.ConfigureLLM(mockLLM{})
	text, err := engine.CompleteLLM(context.Background(), "sys", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if text == "" {
		t.Fatal("empty")
	}
}

func openTestDB(t *testing.T) *sqlite.Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlite.New(sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
