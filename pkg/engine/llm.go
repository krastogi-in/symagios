package engine

import (
	"context"
	"fmt"
	"sync"

	"symagios/contentguard"
	"symagios/engine/hooks"
	"symagios/llm"
	searchgateway "symagios/search-gateway"
)

var (
	llmMu     sync.RWMutex
	llmClient llm.Client
)

// ConfigureLLM installs the shared LLM client (Claude or OpenCodex).
func ConfigureLLM(c llm.Client) {
	llmMu.Lock()
	defer llmMu.Unlock()
	llmClient = c
}

// LLM returns the configured client, or nil if unset.
func LLM() llm.Client {
	llmMu.RLock()
	defer llmMu.RUnlock()
	return llmClient
}

// CompleteLLM marks the system prompt and calls the configured provider.
// Pre/post content guards belong in execute; this only wraps system markers.
func CompleteLLM(ctx context.Context, system, user string) (string, error) {
	c := LLM()
	if c == nil {
		return "", fmt.Errorf("llm client not configured (set SYMAGIOS_LLM_PROVIDER + API key)")
	}

	built := contentguard.WrapSystemPrompt(system) + "\n\n" + user
	req := llm.Request{
		Messages:       []llm.Message{{Role: "user", Content: built}},
		GovernedSearch: governedSearchForLLM,
	}
	if sys, untrusted, ok := contentguard.SplitSystemAndUntrusted(built); ok {
		req.System = sys
		req.Messages = []llm.Message{{Role: "user", Content: untrusted}}
	}

	resp, err := c.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

func governedSearchForLLM(ctx context.Context, query string) (string, error) {
	if _, err := hooks.ProtectToolCall(searchgateway.ToolName, query); err != nil {
		return "", err
	}
	out := CallSearchBroker(ctx, query)
	if out.Unavailable {
		msg := out.Message
		if msg == "" {
			msg = "search not available"
		}
		return "", fmt.Errorf("%s", msg)
	}
	return out.Text, nil
}
