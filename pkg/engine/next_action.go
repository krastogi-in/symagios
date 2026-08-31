package engine

import (
	"regexp"
	"strings"

	"symagios/utils/types"
)

// NextActionKind is a control directive from an LLM reply — not an ActionType.
type NextActionKind string

const (
	NextAgent      NextActionKind = "AGENT"
	NextSkill      NextActionKind = "SKILL"
	NextSubAgent   NextActionKind = "SUB_AGENT"
	NextScript     NextActionKind = "SCRIPT"
	NextWebSearch  NextActionKind = "WEB_SEARCH"
	NextMCP        NextActionKind = "MCP"
	NextHumanInput NextActionKind = "NEED_HUMAN_INPUT"
)

// NextAction is one structured follow-up parsed from an LLM reply.
// Format: KIND::name{json}
type NextAction struct {
	Kind    NextActionKind
	Name    string
	Payload string // JSON object body (may be empty → "{}")
	HITL    bool
}

var nextActionPatterns = []struct {
	kind NextActionKind
	hitl bool
	re   *regexp.Regexp
}{
	{NextHumanInput, true, regexp.MustCompile(`(?s)NEED_HUMAN_INPUT::([a-zA-Z0-9_-]+)\s*\{([\s\S]*?)\}`)},
	{NextSubAgent, false, regexp.MustCompile(`(?s)SUB_AGENT::([a-zA-Z0-9_-]+)\s*\{([\s\S]*?)\}`)},
	{NextSkill, false, regexp.MustCompile(`(?s)SKILL::([a-zA-Z0-9_-]+)\s*\{([\s\S]*?)\}`)},
	{NextScript, false, regexp.MustCompile(`(?s)SCRIPT::([a-zA-Z0-9_-]+)\s*\{([\s\S]*?)\}`)},
	{NextWebSearch, false, regexp.MustCompile(`(?s)WEB_SEARCH::([a-zA-Z0-9_-]+)\s*\{([\s\S]*?)\}`)},
	{NextMCP, false, regexp.MustCompile(`(?s)MCP::([a-zA-Z0-9_-]+)\s*\{([\s\S]*?)\}`)},
	{NextAgent, false, regexp.MustCompile(`(?s)AGENT::([a-zA-Z0-9_-]+)\s*\{([\s\S]*?)\}`)},
}

// ParseNextAction finds the first structured action in text.
func ParseNextAction(text string) (NextAction, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return NextAction{}, false
	}
	for _, p := range nextActionPatterns {
		m := p.re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		payload := strings.TrimSpace(m[2])
		if payload == "" {
			payload = "{}"
		}
		return NextAction{
			Kind:    p.kind,
			Name:    m[1],
			Payload: payload,
			HITL:    p.hitl,
		}, true
	}
	return NextAction{}, false
}

// ChildActionType maps orchestration next-actions to ActionType. Non-child kinds return ok=false.
func (a NextAction) ChildActionType() (types.ActionType, bool) {
	switch a.Kind {
	case NextAgent:
		return types.Agent, true
	case NextSkill:
		return types.Skill, true
	case NextSubAgent:
		return types.SubAgent, true
	default:
		return "", false
	}
}

var nextActionStripRE = regexp.MustCompile(
	`(?s)\b(?:NEED_HUMAN_INPUT|SUB_AGENT|SKILL|SCRIPT|WEB_SEARCH|MCP|AGENT)::[a-zA-Z0-9_-]+\s*\{[\s\S]*?\}`,
)

var summaryStripRE = regexp.MustCompile(
	`(?s)\bSUMMARY::(?:[a-zA-Z0-9_-]+\s*)?(?:\{[\s\S]*?\}|[^\n]*)`,
)

// StripNextActions removes structured next-action directives from text.
func StripNextActions(text string) string {
	return stripInternalDirectives(text)
}

func stripInternalDirectives(text string) string {
	if text == "" {
		return ""
	}
	out := nextActionStripRE.ReplaceAllString(text, "")
	out = summaryStripRE.ReplaceAllString(out, "")
	out = strings.TrimSpace(out)
	out = regexp.MustCompile(`\n{3,}`).ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out)
}

// PublicFacingText is text safe to show operators/callers/providers.
func PublicFacingText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if act, ok := ParseNextAction(text); ok && act.HITL {
		stripped := stripInternalDirectives(text)
		if stripped != "" {
			return stripped
		}
		if act.Name != "" && act.Payload != "" && act.Payload != "{}" {
			return act.Name + ": " + act.Payload
		}
		if act.Payload != "" && act.Payload != "{}" {
			return act.Payload
		}
		if act.Name != "" {
			return act.Name
		}
		return "Human input required"
	}
	return stripInternalDirectives(text)
}
