package searchgateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	searchgateway "symagios/search-gateway"
)

func TestToolSchemas(t *testing.T) {
	if searchgateway.AnthropicTool()["name"] != searchgateway.ToolName {
		t.Fatal("anthropic tool name")
	}
	if searchgateway.OpenAITool()["name"] != searchgateway.ToolName {
		t.Fatal("openai tool name")
	}
	fn, _ := searchgateway.OpenAIChatTool()["function"].(map[string]any)
	if fn["name"] != searchgateway.ToolName {
		t.Fatal("openai chat tool name")
	}
	if searchgateway.GeminiTool()["name"] != searchgateway.ToolName {
		t.Fatal("gemini tool name")
	}
}

func TestFormatForTool(t *testing.T) {
	text := searchgateway.FormatForTool([]searchgateway.Result{
		{Title: "A", URL: "https://a.example", Content: "body"},
	}, "summary")
	if text == "" {
		t.Fatal("empty format")
	}
}

func TestHandlerValidation(t *testing.T) {
	h := searchgateway.NewHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/search", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/search", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		// nil body decode fails
		_ = json.RawMessage{}
		if rr.Code != http.StatusBadRequest && rr.Code != http.StatusBadGateway {
			t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
		}
	}
}
