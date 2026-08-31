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

// OpenCodex uses an OpenAI-compatible Chat Completions API (Codex / GPT endpoints).
type openCodexClient struct {
	cfg  Config
	http *http.Client
}

func newOpenCodex(cfg Config, httpClient *http.Client) *openCodexClient {
	return &openCodexClient{cfg: cfg, http: httpClient}
}

func (c *openCodexClient) Provider() Provider { return ProviderOpenCodex }

func (c *openCodexClient) Complete(ctx context.Context, req Request) (Response, error) {
	if req.GovernedSearch != nil {
		return c.completeWithClientSearch(ctx, req)
	}
	return c.completePlain(ctx, req)
}

func (c *openCodexClient) completePlain(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = c.cfg.Model
	}
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = c.cfg.MaxTokens
	}

	msgs := make([]map[string]string, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		role := m.Role
		if role != "assistant" && role != "system" {
			role = "user"
		}
		msgs = append(msgs, map[string]string{"role": role, "content": m.Content})
	}
	if len(msgs) == 0 {
		return Response{}, fmt.Errorf("opencodex: no messages")
	}

	body := map[string]any{
		"model":      model,
		"messages":   msgs,
		"max_tokens": maxTok,
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
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Response{}, err
	}
	text := ""
	if len(parsed.Choices) > 0 {
		text = parsed.Choices[0].Message.Content
	}
	return Response{Text: text, Model: firstNonEmpty(parsed.Model, model), Raw: string(respBody)}, nil
}

func (c *openCodexClient) completeWithClientSearch(ctx context.Context, req Request) (Response, error) {
	tools, err := clientSearchTools(ProviderOpenCodex)
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
	sys := req.System
	note := "Live web search is only the web_search function. Do not use provider built-in search."
	if sys != "" {
		sys += "\n\n" + note
	} else {
		sys = note
	}
	msgs := make([]map[string]any, 0, len(req.Messages)+8)
	msgs = append(msgs, map[string]any{"role": "system", "content": sys})
	for _, m := range req.Messages {
		if m.Role == "system" {
			continue
		}
		role := m.Role
		if role != "assistant" {
			role = "user"
		}
		msgs = append(msgs, map[string]any{"role": role, "content": m.Content})
	}

	var last Response
	for round := 0; round < searchRounds(req); round++ {
		body := map[string]any{
			"model":       model,
			"messages":    msgs,
			"max_tokens":  maxTok,
			"tools":       tools,
			"tool_choice": "auto",
		}
		parsed, raw, err := c.postChatRaw(ctx, body)
		if err != nil {
			return Response{}, err
		}
		if len(parsed.Choices) == 0 {
			return Response{}, fmt.Errorf("opencodex: empty choices")
		}
		msg := parsed.Choices[0].Message
		last = Response{Text: msg.Content, Model: firstNonEmpty(parsed.Model, model), Raw: raw}
		if len(msg.ToolCalls) == 0 {
			return last, nil
		}
		msgs = append(msgs, map[string]any{
			"role":       "assistant",
			"content":    msg.Content,
			"tool_calls": msg.ToolCalls,
		})
		for _, tc := range msg.ToolCalls {
			out, err := runGovernedSearch(ctx, req.GovernedSearch, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			if err != nil {
				out = "search error: " + err.Error()
			}
			msgs = append(msgs, map[string]any{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      out,
			})
		}
	}
	return last, fmt.Errorf("opencodex: governed search exceeded max rounds")
}

type openAIChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *openCodexClient) postChatRaw(ctx context.Context, body map[string]any) (openAIChatResponse, string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return openAIChatResponse{}, "", err
	}
	respBody, err := c.postRaw(ctx, raw)
	if err != nil {
		return openAIChatResponse{}, "", err
	}
	var parsed openAIChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return openAIChatResponse{}, "", err
	}
	return parsed, string(respBody), nil
}

func (c *openCodexClient) postRaw(ctx context.Context, raw []byte) ([]byte, error) {
	target := strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	if err := egress.SetSource(httpReq, c.cfg.Source); err != nil {
		return nil, err
	}
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
		return nil, fmt.Errorf("opencodex: %s: %s", resp.Status, truncate(string(respBody), 400))
	}
	return respBody, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
