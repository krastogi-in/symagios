package hooks

import (
	"sync"

	"symagios/contentguard"
)

var (
	contentMu       sync.RWMutex
	contentPipeline *contentguard.Pipeline
)

// ConfigureContentGuards installs the pre/post LLM content pipeline.
func ConfigureContentGuards(cfg contentguard.Config) {
	contentMu.Lock()
	defer contentMu.Unlock()
	if !cfg.Enabled {
		contentPipeline = nil
		return
	}
	contentPipeline = contentguard.New(cfg)
}

// ProtectPreLLM redacts or blocks sensitive content before it is sent to an LLM.
func ProtectPreLLM(text string) (contentguard.Result, error) {
	return protect(contentguard.PreLLM, text)
}

// ProtectPostLLM redacts or blocks sensitive content in LLM output / tool results.
func ProtectPostLLM(text string) (contentguard.Result, error) {
	return protect(contentguard.PostLLM, text)
}

// ProtectToolCall re-checks arguments before a tool runs.
// Egress tools such as send-email are blocked when the payload is sensitive,
// marked restricted, or hides data in Unicode tag characters.
func ProtectToolCall(tool, payload string) (contentguard.Result, error) {
	contentMu.RLock()
	p := contentPipeline
	contentMu.RUnlock()
	if p == nil {
		return contentguard.Result{Text: payload}, nil
	}
	return p.ProtectToolCall(tool, payload)
}

// BuildGuardedPrompt wraps system instructions in the Symagios system-generated
// marker and scrubbing untrusted user text so jailbreaks cannot override it.
func BuildGuardedPrompt(systemInstructions, userOrMemory string) (contentguard.Result, error) {
	contentMu.RLock()
	p := contentPipeline
	contentMu.RUnlock()
	if p == nil {
		return contentguard.Result{
			Text: contentguard.WrapSystemPrompt(systemInstructions) + "\n\n" + userOrMemory,
		}, nil
	}
	return p.BuildGuardedPrompt(systemInstructions, userOrMemory)
}

func protect(dir contentguard.Direction, text string) (contentguard.Result, error) {
	contentMu.RLock()
	p := contentPipeline
	contentMu.RUnlock()
	if p == nil {
		return contentguard.Result{Text: text}, nil
	}
	return p.Protect(dir, text)
}
