package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"symagios/egress"
)

type claudeClient struct {
	cfg  Config
	http *http.Client
}

func newClaude(cfg Config, httpClient *http.Client) *claudeClient {
	return &claudeClient{cfg: cfg, http: httpClient}
}

func (c *claudeClient) Provider() Provider { return c.cfg.Provider }

func (c *claudeClient) Complete(ctx context.Context, req Request) (Response, error) {
	if req.GovernedSearch != nil {
		return c.completeWithClientSearch(ctx, req)
	}
	return c.completePlain(ctx, req)
}

func (c *claudeClient) completePlain(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = c.cfg.Model
	}
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = c.cfg.MaxTokens
	}

	system := req.System
	msgs := make([]map[string]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := m.Role
		if role == "system" {
			if system == "" {
				system = m.Content
			} else {
				system += "\n\n" + m.Content
			}
			continue
		}
		if role != "assistant" {
			role = "user"
		}
		msgs = append(msgs, map[string]string{"role": role, "content": m.Content})
	}
	if len(msgs) == 0 {
		return Response{}, fmt.Errorf("claude: no user/assistant messages")
	}

	body := map[string]any{
		"model":      model,
		"max_tokens": maxTok,
		"messages":   msgs,
	}
	if system != "" {
		body["system"] = system
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}

	respBody, err := c.postRaw(ctx, raw)
	if err != nil {
		return Response{}, err
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Response{}, err
	}
	var text strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" || block.Type == "" {
			text.WriteString(block.Text)
		}
	}
	return Response{Text: text.String(), Model: firstNonEmpty(parsed.Model, model), Raw: string(respBody)}, nil
}

func (c *claudeClient) completeWithClientSearch(ctx context.Context, req Request) (Response, error) {
	tools, err := clientSearchTools(c.cfg.Provider)
	if err != nil {
		return Response{}, err
	}
	model := req.Model
	if model == "" {
		model = c.cfg.Model
	}
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = c.cfg.MaxTokens
	}

	system := req.System
	msgs := make([]any, 0, len(req.Messages)+8)
	for _, m := range req.Messages {
		role := m.Role
		if role == "system" {
			if system == "" {
				system = m.Content
			} else {
				system += "\n\n" + m.Content
			}
			continue
		}
		if role != "assistant" {
			role = "user"
		}
		msgs = append(msgs, map[string]any{"role": role, "content": m.Content})
	}
	if len(msgs) == 0 {
		return Response{}, fmt.Errorf("claude: no user/assistant messages")
	}
	note := "Live web search is only the web_search tool. Do not assume Anthropic server-side search."
	if system != "" {
		system += "\n\n" + note
	} else {
		system = note
	}

	var last Response
	for round := 0; round < searchRounds(req); round++ {
		body := map[string]any{
			"model":       model,
			"max_tokens":  maxTok,
			"messages":    msgs,
			"tools":       tools,
			"tool_choice": map[string]any{"type": "auto"},
			"system":      system,
		}
		parsed, raw, err := c.postMessages(ctx, body)
		if err != nil {
			return Response{}, err
		}
		text, toolUses := splitClaudeContent(parsed.Content)
		last = Response{Text: text, Model: firstNonEmpty(parsed.Model, model), Raw: raw}
		if len(toolUses) == 0 {
			return last, nil
		}
		msgs = append(msgs, map[string]any{"role": "assistant", "content": parsed.Content})
		results := make([]any, 0, len(toolUses))
		for _, tu := range toolUses {
			if tu.Name == "provider-server-search" {
				return Response{}, fmt.Errorf("claude: provider server web search is not allowed")
			}
			out, err := runGovernedSearch(ctx, req.GovernedSearch, tu.Name, tu.Input)
			if err != nil {
				out = "search error: " + err.Error()
			}
			results = append(results, map[string]any{
				"type":        "tool_result",
				"tool_use_id": tu.ID,
				"content":     out,
			})
		}
		msgs = append(msgs, map[string]any{"role": "user", "content": results})
	}
	return last, fmt.Errorf("claude: governed search exceeded max rounds")
}

type claudeAPIResponse struct {
	Content []json.RawMessage `json:"content"`
	Model   string            `json:"model"`
}

type claudeToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (c *claudeClient) postMessages(ctx context.Context, body map[string]any) (claudeAPIResponse, string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return claudeAPIResponse{}, "", err
	}
	respBody, err := c.postRaw(ctx, raw)
	if err != nil {
		return claudeAPIResponse{}, "", err
	}
	var parsed claudeAPIResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return claudeAPIResponse{}, "", err
	}
	return parsed, string(respBody), nil
}

func (c *claudeClient) postRaw(ctx context.Context, raw []byte) ([]byte, error) {
	if c.cfg.Provider == ProviderClaudeVertex {
		return c.postVertexRaw(ctx, raw)
	}
	target := strings.TrimRight(c.cfg.BaseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if err := egress.SetSource(httpReq, c.cfg.Source); err != nil {
		return nil, err
	}
	return c.doLLM(httpReq)
}

// postVertexRaw rewrites a Messages-style JSON body for Vertex :rawPredict
// (model → URL; anthropic_version required; Bearer Google token).
func (c *claudeClient) postVertexRaw(ctx context.Context, raw []byte) ([]byte, error) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	model, _ := body["model"].(string)
	if model == "" {
		model = c.cfg.Model
	}
	delete(body, "model")
	body["anthropic_version"] = vertexAnthropicVersion
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	target, err := vertexRawPredictURL(c.cfg.Project, c.cfg.Location, model)
	if err != nil {
		return nil, err
	}
	if override := strings.TrimSpace(c.cfg.BaseURL); override != "" {
		// Optional full URL override (tests); must already be the :rawPredict URL.
		target = strings.TrimRight(override, "/")
	}
	token, err := vertexBearerToken(ctx, c.cfg)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if err := egress.SetSource(httpReq, c.cfg.Source); err != nil {
		return nil, err
	}
	return c.doLLM(httpReq)
}

func (c *claudeClient) doLLM(httpReq *http.Request) ([]byte, error) {
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("claude: %s: %s", resp.Status, truncate(string(respBody), 400))
	}
	return respBody, nil
}

func splitClaudeContent(blocks []json.RawMessage) (text string, tools []claudeToolUse) {
	var b strings.Builder
	for _, raw := range blocks {
		var head struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			continue
		}
		switch head.Type {
		case "text", "":
			b.WriteString(head.Text)
		case "server_tool_use", "web_search_tool_result":
			// Provider executed search. Treat as a hard failure so it cannot pass through.
			tools = append(tools, claudeToolUse{ID: head.ID, Name: "provider-server-search", Input: raw})
		case "tool_use":
			tools = append(tools, claudeToolUse{ID: head.ID, Name: head.Name, Input: head.Input})
		}
	}
	return b.String(), tools
}
