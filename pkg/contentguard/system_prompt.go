package contentguard

import (
	"fmt"
	"strings"
)

// System-generated prompt markers. Only Symagios-authored instructions should
// be wrapped with these. User/memory text that contains them is treated as forgery.
const (
	SystemPromptBegin = "<<<SYMAGIOS_SYSTEM_PROMPT>>>"
	SystemPromptEnd   = "<<<END_SYMAGIOS_SYSTEM_PROMPT>>>"
)

// WrapSystemPrompt marks text as Symagios-authored system instructions.
// Models and filters treat this block as authoritative; jailbreak attempts outside it
// cannot "become" system text.
func WrapSystemPrompt(body string) string {
	body = strings.TrimSpace(body)
	return SystemPromptBegin + "\n" + body + "\n" + SystemPromptEnd
}

// IsSystemGenerated reports whether text contains our marked system block.
func IsSystemGenerated(text string) bool {
	return strings.Contains(text, SystemPromptBegin) && strings.Contains(text, SystemPromptEnd)
}

// StripForgedSystemMarkers removes system markers from untrusted text so a user
// or stored memory cannot pretend to be the system prompt.
func StripForgedSystemMarkers(text string) string {
	text = strings.ReplaceAll(text, SystemPromptBegin, "[FORGED_SYSTEM_MARKER]")
	text = strings.ReplaceAll(text, SystemPromptEnd, "[FORGED_SYSTEM_MARKER]")
	return text
}

// SplitSystemAndUntrusted separates our marked system block from the rest.
// If markers are missing or malformed, the whole string is untrusted.
func SplitSystemAndUntrusted(text string) (system, untrusted string, ok bool) {
	start := strings.Index(text, SystemPromptBegin)
	end := strings.Index(text, SystemPromptEnd)
	if start < 0 || end < 0 || end < start {
		return "", text, false
	}
	sysBodyStart := start + len(SystemPromptBegin)
	system = strings.TrimSpace(text[sysBodyStart:end])
	before := text[:start]
	after := text[end+len(SystemPromptEnd):]
	untrusted = before + after
	return system, untrusted, true
}

// BuildGuardedPrompt puts system instructions in a marked block and appends
// scrubbed untrusted user/memory content so jailbreaks cannot override the system text.
func (p *Pipeline) BuildGuardedPrompt(systemInstructions, userOrMemory string) (Result, error) {
	if p == nil || !p.cfg.Enabled {
		return Result{Text: WrapSystemPrompt(systemInstructions) + "\n\n" + userOrMemory}, nil
	}

	scrubbed, err := p.Protect(StoreMemory, userOrMemory)
	if err != nil && scrubbed.Blocked {
		return scrubbed, err
	}
	if scrubbed.Blocked {
		return scrubbed, fmt.Errorf("contentguard: untrusted prompt content blocked: %s", scrubbed.Reason)
	}

	out := WrapSystemPrompt(systemInstructions) + "\n\n" + scrubbed.Text
	return Result{Text: out, Findings: scrubbed.Findings}, nil
}
