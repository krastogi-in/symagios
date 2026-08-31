package mcpgateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpgateway "symagios/mcp-gateway"
)

func TestMissingToolNeedsHuman(t *testing.T) {
	reg := mcpgateway.NewRegistry()
	_ = reg.Attach(mcpgateway.ServerSpec{
		ID:      "demo",
		Enabled: true,
		BaseURL: "stub://",
		Tools:   []mcpgateway.ToolSpec{{Name: "echo"}},
	})
	svc := mcpgateway.New(mcpgateway.Config{ContentGuards: false}, reg)

	_, errBody := svc.Call(context.Background(), mcpgateway.CallRequest{Tool: "not_attached"})
	if errBody == nil {
		t.Fatal("expected error")
	}
	if errBody.Code != mcpgateway.CodeToolNotFound || !errBody.NeedHuman {
		t.Fatalf("expected tool_not_found + need_human, got %#v", errBody)
	}
}

func TestStubCallAttachedTool(t *testing.T) {
	reg := mcpgateway.NewRegistry()
	_ = reg.Attach(mcpgateway.ServerSpec{
		ID:      "demo",
		Enabled: true,
		BaseURL: "stub://",
		Tools:   []mcpgateway.ToolSpec{{Name: "echo"}},
	})
	svc := mcpgateway.New(mcpgateway.Config{ContentGuards: false}, reg)

	resp, errBody := svc.Call(context.Background(), mcpgateway.CallRequest{
		Tool:      "echo",
		Arguments: json.RawMessage(`{"msg":"hi"}`),
	})
	if errBody != nil {
		t.Fatalf("unexpected: %#v", errBody)
	}
	if resp.Server != "demo" || !strings.Contains(resp.Result, "hi") {
		t.Fatalf("bad response: %#v", resp)
	}
}

func TestHTTPCallMissingReturnsConflict(t *testing.T) {
	reg := mcpgateway.NewRegistry()
	svc := mcpgateway.New(mcpgateway.Config{ContentGuards: false}, reg)
	mux := http.NewServeMux()
	mcpgateway.NewHandler(svc).Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/tools/call", strings.NewReader(`{"tool":"missing"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body mcpgateway.ErrorBody
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.NeedHuman || body.Code != mcpgateway.CodeToolNotFound {
		t.Fatalf("%#v", body)
	}
}

func TestAttachAndList(t *testing.T) {
	reg := mcpgateway.NewRegistry()
	svc := mcpgateway.New(mcpgateway.Config{ContentGuards: false}, reg)
	mux := http.NewServeMux()
	mcpgateway.NewHandler(svc).Mount(mux)

	body := `{"id":"x","enabled":true,"base_url":"stub://","tools":[{"name":"ping"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/attach", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("attach: %d %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/mcp/tools", nil)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), "ping") {
		t.Fatalf("list: %s", rr.Body.String())
	}
}
