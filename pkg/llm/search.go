package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	searchgateway "symagios/search-gateway"
)

func searchRounds(req Request) int {
	if req.MaxSearchRounds > 0 {
		return req.MaxSearchRounds
	}
	return 4
}

// clientSearchTools is the only tool list we send. It must stay a custom
// client tool. Anthropic's server tool is {"type":"web_search_20250305",...};
// OpenAI's is {"type":"web_search"}. Neither shape is included here.
func clientSearchTools(provider Provider) ([]any, error) {
	var tool map[string]any
	switch provider {
	case ProviderClaude, ProviderClaudeVertex:
		tool = searchgateway.AnthropicTool()
	case ProviderOpenCodex:
		tool = searchgateway.OpenAIChatTool()
	default:
		return nil, fmt.Errorf("llm: no governed search tool for %s", provider)
	}
	if err := rejectProviderSearchTool(tool); err != nil {
		return nil, err
	}
	return []any{tool}, nil
}

func rejectProviderSearchTool(tool map[string]any) error {
	typ, _ := tool["type"].(string)
	switch typ {
	case "web_search", "web_search_20250305", "web_search_20260209":
		return fmt.Errorf("refusing provider server web search tool type %q", typ)
	}
	// Nested OpenAI chat tools use type "function", which is a client tool.
	return nil
}

func queryFromToolArgs(raw json.RawMessage) (string, error) {
	var obj struct {
		Query string `json:"query"`
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("web_search: query is required")
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", fmt.Errorf("web_search args: %w", err)
	}
	q := strings.TrimSpace(obj.Query)
	if q == "" {
		return "", fmt.Errorf("web_search: query is required")
	}
	return q, nil
}

func runGovernedSearch(ctx context.Context, fn func(context.Context, string) (string, error), name string, args json.RawMessage) (string, error) {
	if name != searchgateway.ToolName {
		return "", fmt.Errorf("refusing tool %q; only %s is allowed", name, searchgateway.ToolName)
	}
	if fn == nil {
		return "", fmt.Errorf("governed search handler not configured")
	}
	q, err := queryFromToolArgs(args)
	if err != nil {
		return "", err
	}
	return fn(ctx, q)
}
