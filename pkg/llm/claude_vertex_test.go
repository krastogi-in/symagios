package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVertexRawPredictURL(t *testing.T) {
	u, err := vertexRawPredictURL("p1", "us-east5", "claude-sonnet-4@20250514")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://us-east5-aiplatform.googleapis.com/v1/projects/p1/locations/us-east5/publishers/anthropic/models/claude-sonnet-4@20250514:rawPredict"
	if u != want {
		t.Fatalf("got %s", u)
	}
	u, err = vertexRawPredictURL("p1", "global", "claude-opus-4")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, "https://aiplatform.googleapis.com/") || !strings.Contains(u, "/locations/global/") {
		t.Fatalf("global url=%s", u)
	}
	u, err = vertexRawPredictURL("p1", "us", "claude-sonnet-4@20250514")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, "https://aiplatform.us.rep.googleapis.com/") {
		t.Fatalf("us rep url=%s", u)
	}
}

func TestClaudeVertexPostsRawPredict(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": "hi from vertex"}},
			"model":   "claude-sonnet-4@20250514",
		})
	}))
	defer srv.Close()

	c := newClaude(Config{
		Provider:  ProviderClaudeVertex,
		APIKey:    "ya29.test-token",
		Project:   "demo-proj",
		Location:  "us-east5",
		Model:     "claude-sonnet-4@20250514",
		BaseURL:   srv.URL + "/v1/projects/demo-proj/locations/us-east5/publishers/anthropic/models/claude-sonnet-4@20250514:rawPredict",
		MaxTokens: 64,
	}, srv.Client())

	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hi from vertex" {
		t.Fatalf("text=%q", resp.Text)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ya29.") {
		t.Fatalf("auth=%q", gotAuth)
	}
	if !strings.Contains(gotPath, ":rawPredict") {
		t.Fatalf("path=%q", gotPath)
	}
	if strings.Contains(gotBody, `"model"`) {
		t.Fatalf("vertex body must not include model: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"anthropic_version":"vertex-2023-10-16"`) {
		t.Fatalf("body=%s", gotBody)
	}
}
