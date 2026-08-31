package contentguard

import "regexp"

// NewPromptExtractionScanner flags attempts to leak the system prompt:
// direct/reframed asks, translation/encoding wrappers, payload splitting,
// error-message fishing, and behavioral rule reconstruction probes.
func NewPromptExtractionScanner() Scanner {
	raw := []jailbreakPattern{
		// Direct ask / reframed extraction
		{`(?i)repeat\s+(the\s+)?(text|content|message|prompt|instructions?)\s+(above|below|earlier|before)`, "prompt_extract"},
		{`(?i)repeat\s+(everything|all)\s+(above|before)\s+starting\s+with`, "prompt_extract"},
		{`(?i)starting\s+with\s+["']?you\s+are["']?`, "prompt_extract"},
		{`(?i)output\s+(your\s+)?(system\s+)?(prompt|instructions?)\s+(in\s+)?(a\s+)?(code\s+block|markdown|json|xml|yaml)`, "prompt_extract"},
		{`(?i)(print|show|dump|display|reveal|echo|write\s+out)\s+(your\s+)?(system\s+)?(prompt|instructions?|rules)`, "prompt_extract"},
		{`(?i)what\s+(were|are)\s+you\s+(told|given|instructed)\s+(before|prior\s+to)\s+(this\s+)?(conversation|chat|session)?`, "prompt_extract"},
		{`(?i)what\s+(is|are)\s+your\s+(system\s+)?(prompt|instructions?|hidden\s+rules|initial\s+instructions?)`, "prompt_extract"},
		{`(?i)(verbatim|exact(ly)?)\s+(copy|quote|reproduce|repeat)\s+(of\s+)?(your\s+)?(system\s+)?(prompt|instructions?)`, "prompt_extract"},
		{`(?i)(spell\s+out|recite|regurgitate)\s+(your\s+)?(system\s+)?(prompt|instructions?)`, "prompt_extract"},
		{`(?i)(begin|start)\s+(your\s+)?(reply|response|answer)\s+with\s+["']?you\s+are`, "prompt_extract"},
		{`(?i)ignore\s+the\s+instruction\s+(not\s+)?to\s+(reveal|share|disclose)\s+(your\s+)?(prompt|instructions?)`, "prompt_extract"},
		{`(?i)(show|tell)\s+me\s+(the\s+)?(hidden|secret|system)\s+(prompt|instructions?|rules)`, "prompt_extract"},

		// Translation / encoding wrappers around the prompt
		{`(?i)translate\s+(your\s+)?(system\s+)?(prompt|instructions?|rules)\s+(to|into)\s+\w+`, "prompt_extract"},
		{`(?i)(base64|rot13|hex|morse|pig\s*latin)[- ]?(encode|decode)?\s+(your\s+)?(system\s+)?(prompt|instructions?)`, "prompt_extract"},
		{`(?i)(encode|decode)\s+(your\s+)?(system\s+)?(prompt|instructions?).{0,40}(base64|rot13|hex)`, "prompt_extract"},
		{`(?i)summariz(e|ing)\s+(your\s+)?(system\s+)?(prompt|instructions?)\s+(in|into|as)\s+\w+`, "prompt_extract"},
		{`(?i)rewrite\s+(your\s+)?(system\s+)?(prompt|instructions?)\s+(in|as|using)`, "prompt_extract"},
		{`(?i)convert\s+(your\s+)?(system\s+)?(prompt|instructions?)\s+(to|into)`, "prompt_extract"},

		// Payload splitting — fragment asks that dodge whole-prompt refusal
		{`(?i)(what('s| is)\s+)?the\s+(first|second|third|last|next)\s+(sentence|line|paragraph|word|token)\s+of\s+(your\s+)?(system\s+)?(prompt|instructions?)`, "prompt_extract"},
		{`(?i)(give|tell|show)\s+me\s+(just\s+)?(the\s+)?(first|second|third|next)\s+(sentence|line|part|half)\s+of\s+(your\s+)?(instructions?|prompt)`, "prompt_extract"},
		{`(?i)(now|then)\s+(the\s+)?(second|third|next|remaining)\s+(sentence|line|part)`, "prompt_extract"},
		{`(?i)(continue|keep\s+going)\s+(with|from)\s+(the\s+)?(next|following)\s+(sentence|line)\s+of\s+(your\s+)?(prompt|instructions?)`, "prompt_extract"},
		{`(?i)(one\s+sentence\s+at\s+a\s+time|piece\s+by\s+piece).{0,40}(prompt|instructions?)`, "prompt_extract"},
		{`(?i)(don'?t|do\s+not)\s+(print|show|give)\s+(the\s+)?whole\s+(prompt|instructions?).{0,40}(just|only)\s+(the\s+)?(first|next)`, "prompt_extract"},

		// Error-message / debug fishing
		{`(?i)(trigger|cause|force)\s+(a\s+)?(verbose\s+)?(error|exception|stack\s*trace|debug)`, "prompt_extract"},
		{`(?i)(show|print|dump)\s+(me\s+)?(your\s+)?(debug|config(uration)?|env(ironment)?|system\s+info)`, "prompt_extract"},
		{`(?i)(malformed|invalid)\s+(input|json|prompt).{0,60}(echo|print|show|dump|reveal).{0,40}(config|prompt|instructions?)`, "prompt_extract"},
		{`(?i)what\s+(error|exception|debug)\s+(message|output).{0,40}(config|prompt|instructions?|secrets?)`, "prompt_extract"},
		{`(?i)(enable|turn\s+on)\s+(debug|verbose|developer)\s+mode.{0,40}(prompt|config|instructions?)`, "prompt_extract"},
		{`(?i)(__debug__|traceback|stacktrace).{0,40}(system\s+prompt|instructions?|config)`, "prompt_extract"},

		// Indirect leakage via behavior / rule reconstruction
		{`(?i)(compare|contrast)\s+(your|how\s+you)\s+(behavio[u]?r|rules|policies).{0,60}(edge\s+case|without\s+your\s+rules)`, "prompt_extract"},
		{`(?i)without\s+(quoting|repeating|revealing)\s+(the\s+)?(prompt|text).{0,80}(summarize|describe|list).{0,40}(rules|instructions?|guardrails)`, "prompt_extract"},
		{`(?i)(list|enumerate|describe)\s+(all\s+)?(your\s+)?(hidden\s+)?(rules|constraints|policies|guardrails)`, "prompt_extract"},
		{`(?i)(reconstruct|infer|deduce|reverse[- ]?engineer)\s+(your\s+)?(system\s+)?(prompt|instructions?|rules)`, "prompt_extract"},
		{`(?i)(which|what)\s+(rules?|policies|instructions?)\s+(would|do)\s+you\s+(follow|apply|enforce).{0,40}(if|when)`, "prompt_extract"},
		{`(?i)(tell|explain)\s+me\s+(how\s+)?(your\s+)?(safety|content)\s+(rules|filters|policies)\s+(work|decide)`, "prompt_extract"},
		{`(?i)(yes[/ ]?no|true[/ ]?false)\s+(quiz|questions?).{0,60}(your\s+)?(system\s+)?(prompt|instructions?|rules)`, "prompt_extract"},
	}

	s := &promptExtractionScanner{}
	for _, p := range raw {
		s.patterns = append(s.patterns, compiledPattern{
			re:   regexp.MustCompile(p.expr),
			kind: p.kind,
		})
	}
	return s
}

type promptExtractionScanner struct {
	patterns []compiledPattern
}

func (s *promptExtractionScanner) Name() string { return "prompt_extract" }

func (s *promptExtractionScanner) Scan(text string) []Finding {
	var out []Finding
	for _, p := range s.patterns {
		out = append(out, scanRegexp(p.kind, "[PROMPT_EXTRACT]", p.re, text)...)
	}
	return out
}
