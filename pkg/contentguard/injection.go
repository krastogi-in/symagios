package contentguard

import (
	"regexp"
	"strings"
)

// NewInjectionScanner flags the standard jailbreak toolkit:
// instruction override, roleplay/persona hijack, context reframing, crescendo
// escalation language, many-shot compliance priming, and refusal suppression.
func NewInjectionScanner() Scanner {
	return newInjectionScanner()
}

func newInjectionScanner() *injectionScanner {
	raw := []jailbreakPattern{
		// Classic instruction override
		{`(?i)ignore\s+(all\s+)?(previous|prior|above)\s+instructions`, "jailbreak"},
		{`(?i)disregard\s+(all\s+)?(previous|prior|above)\s+instructions`, "jailbreak"},
		{`(?i)forget\s+(all\s+)?(previous|prior|above|your)\s+instructions`, "jailbreak"},
		{`(?i)forget\s+everything\s+(you\s+)?(were\s+)?(told|taught|given)`, "jailbreak"},
		{`(?i)forget\s+(what|the)\s+(you|system)\s+(said|prompt|instructions)`, "jailbreak"},
		{`(?i)you\s+should\s+forget\s+(previous|prior|above|your)\s+instructions`, "jailbreak"},
		{`(?i)reset\s+(your\s+)?(instructions|system\s+prompt|rules)`, "jailbreak"},
		{`(?i)override\s+(the\s+)?(system\s+)?(prompt|instructions|rules)`, "jailbreak"},
		{`(?i)new\s+instructions?\s+(override|replace|supersede)`, "jailbreak"},
		{`(?i)you\s+are\s+now\s+(dan|jailbroken|unrestricted|unfiltered)`, "jailbreak"},
		{`(?i)\bjailbreak\b`, "jailbreak"},
		{`(?i)do\s+not\s+follow\s+(your\s+)?(safety|system|prior)\s+(rules|instructions|prompt)`, "jailbreak"},
		{`(?i)reveal\s+(your\s+)?(system\s+)?prompt`, "jailbreak"},
		{`(?i)print\s+(your\s+)?(system\s+)?prompt`, "jailbreak"},
		{`(?i)exfiltrat(e|ion)`, "jailbreak"},
		{`(?i)developer\s+mode\s+enabled`, "jailbreak"},
		{`(?i)<<\s*sys\s*>>`, "jailbreak"},
		{`(?i)\[\s*system\s*\]`, "jailbreak"},

		// Roleplay / persona hijack
		{`(?i)pretend\s+you\s+are\s+(an?\s+)?(uncensored|unrestricted|unfiltered|amoral|jailbroken)`, "jailbreak_roleplay"},
		{`(?i)pretend\s+you\s+are.{0,100}(with\s+no\s+restrictions|uncensored|unfiltered|unrestricted)`, "jailbreak_roleplay"},
		{`(?i)pretend\s+(to\s+be|you'?re)\s+(an?\s+)?(ai|assistant|model)\s+called\s+\w+`, "jailbreak_roleplay"},
		{`(?i)you\s+are\s+(an?\s+)?(uncensored|unrestricted|unfiltered|amoral)\s+(ai|assistant|llm|model)`, "jailbreak_roleplay"},
		{`(?i)act\s+as\s+(an?\s+)?(uncensored|unrestricted|unfiltered|amoral|evil)\b`, "jailbreak_roleplay"},
		{`(?i)role[- ]?play\s+as\s+(an?\s+)?(uncensored|unrestricted|unfiltered|amoral)`, "jailbreak_roleplay"},
		{`(?i)an?\s+(uncensored|unrestricted|unfiltered)\s+(ai|assistant|model).{0,60}(no\s+restrictions|no\s+rules|no\s+safety)`, "jailbreak_roleplay"},
		{`(?i)persona\s*:\s*(dan|evil|uncensored|jailbreak)`, "jailbreak_roleplay"},
		{`(?i)from\s+now\s+on\s+you\s+(will|must|shall)\s+(ignore|disregard|have\s+no)`, "jailbreak_roleplay"},
		{`(?i)your\s+new\s+persona\s+(is|will\s+be)`, "jailbreak_roleplay"},
		{`(?i)enter\s+(DAN|developer)\s+mode`, "jailbreak_roleplay"},
		{`(?i)do\s+anything\s+now`, "jailbreak_roleplay"},
		{`(?i)pretend\s+(there\s+are\s+)?no\s+(rules|limits|restrictions|safety)`, "jailbreak_roleplay"},
		{`(?i)called\s+\w+\s+with\s+no\s+(rules|limits|restrictions|safety|filters)`, "jailbreak_roleplay"},

		// Context reframing — fiction/simulation + safety bypass cue
		{`(?i)(screenplay|script|novel|fiction|story|movie|roleplay).{0,100}((rules?|policies|guidelines|restrictions)\s+(don'?t|do\s+not)\s+apply|no\s+restrictions|ignore\s+(safety|rules)|uncensored)`, "jailbreak_reframe"},
		{`(?i)(clinical|medical|security|red[- ]?team|penetration)\s+(training|simulation|exercise|lab).{0,100}((rules?|policies)\s+(don'?t|do\s+not)\s+apply|no\s+restrictions|ignore\s+safety|bypass)`, "jailbreak_reframe"},
		{`(?i)(usual|normal|standard|safety)\s+(rules|policies|guidelines|restrictions)\s+(don'?t|do\s+not)\s+apply`, "jailbreak_reframe"},
		{`(?i)(rules|policies|guidelines|restrictions)\s+(don'?t|do\s+not)\s+apply\s+(here|in\s+this|for\s+this)`, "jailbreak_reframe"},
		{`(?i)for\s+(educational|academic|research)\s+purposes\s+only.{0,60}(ignore|bypass|no\s+restrictions)`, "jailbreak_reframe"},
		{`(?i)hypothetical(ly)?\s+(scenario|situation).{0,80}(no\s+restrictions|ignore\s+(safety|rules)|uncensored)`, "jailbreak_reframe"},
		{`(?i)in[- ]universe\s+(rules|ethics)\s+(don'?t|do\s+not)\s+apply`, "jailbreak_reframe"},
		{`(?i)suspend\s+(your\s+)?(safety|content)\s+(filters?|policies|guidelines)`, "jailbreak_reframe"},
		{`(?i)as\s+a\s+thought\s+experiment.{0,60}(ignore|bypass|no\s+filter|uncensored)`, "jailbreak_reframe"},

		// Gradual escalation / crescendo — require a bypass/escalation cue
		{`(?i)(let'?s|we\s+can)\s+(take\s+this|go)\s+(a\s+step|one\s+step)\s+further.{0,80}(explicit|uncensored|no\s+rules|bypass|ignore\s+safety|restricted)`, "jailbreak_crescendo"},
		{`(?i)building\s+on\s+(what\s+you\s+(just\s+)?(said|wrote|provided)).{0,80}(more\s+explicit|no\s+restrictions|ignore|bypass|further\s+into)`, "jailbreak_crescendo"},
		{`(?i)now\s+that\s+(we'?ve|you\s+have)\s+(established|agreed|confirmed).{0,80}(drop|ignore|bypass|no\s+longer|without\s+(safety|rules))`, "jailbreak_crescendo"},
		{`(?i)(gradually|slowly)\s+(escalat\w*|push(ing)?\s+(further|harder)).{0,40}(restrict|safety|censor|filter|policy)`, "jailbreak_crescendo"},
		{`(?i)each\s+(turn|message|reply)\s+(get|go|become)s?\s+(more\s+explicit|less\s+filtered|closer\s+to)`, "jailbreak_crescendo"},
		{`(?i)keep\s+(going|pushing)\s+(further|harder).{0,40}(past|beyond)\s+(safety|filters?|rules|policies)`, "jailbreak_crescendo"},
		{`(?i)don'?t\s+stop\s+(at|with)\s+(the\s+)?(safe|censored|filtered)\s+(version|answer)`, "jailbreak_crescendo"},
		{`(?i)more\s+explicit\s+(this\s+time|now|please).{0,40}(no\s+(filter|safety|disclaimer)|uncensored)`, "jailbreak_crescendo"},
		{`(?i)drop\s+the\s+(safety|caution|hedging|disclaimers?)`, "jailbreak_crescendo"},

		// Refusal suppression
		{`(?i)never\s+say\s+["']?(I\s+can'?t|I\s+cannot|I'?m\s+sorry|as\s+an\s+AI)`, "jailbreak_refusal"},
		{`(?i)do\s+not\s+(say|respond\s+with|use)\s+["']?(I\s+can'?t|I\s+cannot|I'?m\s+sorry|I\s+apologize)`, "jailbreak_refusal"},
		{`(?i)(avoid|suppress|skip)\s+(any\s+)?(refusal|apology|disclaimer|safety\s+warning)`, "jailbreak_refusal"},
		{`(?i)(answer|respond|reply)\s+(only\s+)?(with|in)\s+(the\s+)?(requested|exact)\s+(format|content)\s+without\s+(refusal|apology|warning)`, "jailbreak_refusal"},
		{`(?i)you\s+must\s+(always\s+)?(comply|obey|answer)\s+(and\s+)?never\s+(refuse|decline|apologize)`, "jailbreak_refusal"},
		{`(?i)start\s+(your\s+)?(answer|response)\s+with\s+["']?sure[,!]?\s+here`, "jailbreak_refusal"},
		{`(?i)no\s+(moral|ethical)\s+(lectures?|warnings?|disclaimers?)`, "jailbreak_refusal"},
		{`(?i)omit\s+(all\s+)?(safety|content)\s+(warnings?|disclaimers?)`, "jailbreak_refusal"},
	}

	s := &injectionScanner{}
	for _, p := range raw {
		s.patterns = append(s.patterns, compiledPattern{
			re:   regexp.MustCompile(p.expr),
			kind: p.kind,
		})
	}
	return s
}

type jailbreakPattern struct {
	expr string
	kind string
}

type compiledPattern struct {
	re   *regexp.Regexp
	kind string
}

type injectionScanner struct {
	patterns []compiledPattern
}

func (s *injectionScanner) Name() string { return "injection" }

func (s *injectionScanner) Scan(text string) []Finding {
	var out []Finding
	out = append(out, s.scanPatternsOnly(text)...)
	out = append(out, scanManyShot(text)...)
	return out
}

func (s *injectionScanner) scanPatternsOnly(text string) []Finding {
	var out []Finding
	for _, p := range s.patterns {
		out = append(out, scanRegexp(p.kind, "[JAILBREAK]", p.re, text)...)
	}
	return out
}

// assistantComplianceRE counts fabricated assistant turns that already "complied".
var assistantComplianceRE = regexp.MustCompile(
	`(?im)^\s*(?:assistant|ai)\s*:\s*(?:sure|certainly|of course|here(?:'s| is)|absolutely|i'?ll help)\b`,
)

const manyShotThreshold = 3

func scanManyShot(text string) []Finding {
	locs := assistantComplianceRE.FindAllStringIndex(text, -1)
	if len(locs) < manyShotThreshold {
		return nil
	}
	start, end := locs[0][0], locs[len(locs)-1][1]
	if start > 0 {
		if i := strings.LastIndex(text[:start], "\n"); i >= 0 {
			start = i + 1
		} else {
			start = 0
		}
	}
	if end < len(text) {
		if i := strings.Index(text[end:], "\n"); i >= 0 {
			end = end + i
		}
	}
	return []Finding{{
		Kind:        "jailbreak_manyshot",
		Start:       start,
		End:         end,
		Replacement: "[JAILBREAK]",
	}}
}

// NewForgedSystemScanner detects forged Symagios system-prompt markers in untrusted text.
func NewForgedSystemScanner() Scanner {
	return forgedSystemScanner{}
}

type forgedSystemScanner struct{}

func (forgedSystemScanner) Name() string { return "forged_system" }

func (forgedSystemScanner) Scan(text string) []Finding {
	var out []Finding
	for _, marker := range []string{SystemPromptBegin, SystemPromptEnd} {
		start := 0
		for {
			i := strings.Index(text[start:], marker)
			if i < 0 {
				break
			}
			abs := start + i
			out = append(out, Finding{
				Kind:        "forged_system_marker",
				Start:       abs,
				End:         abs + len(marker),
				Replacement: "[FORGED_SYSTEM_MARKER]",
			})
			start = abs + len(marker)
		}
	}
	return out
}
