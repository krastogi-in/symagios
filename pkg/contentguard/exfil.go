package contentguard

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// RestrictedMarker stamps text that came from a protected record.
// Egress tools must refuse any payload that still carries it.
const RestrictedMarker = "<<<SYMAGIOS_RESTRICTED>>>"

// NewExfilScanner flags disclosure-plus-agency attacks:
// authority/role-play requests for protected records, query decomposition,
// and Unicode-tag / invisible-character smuggling.
func NewExfilScanner() Scanner {
	return &exfilScanner{
		authority: []*regexp.Regexp{
			regexp.MustCompile(`(?i)as\s+the\s+(presiding\s+)?(judge|prosecutor|attorney|counsel|officer|clerk|administrator)`),
			regexp.MustCompile(`(?i)(presiding\s+judge|in\s+my\s+capacity\s+as)`),
			regexp.MustCompile(`(?i)(need|retrieve|pull|fetch|show|open|send|email)\s+(me\s+)?(the\s+)?(full\s+)?(witness|sealed|protected|confidential|restricted)\s+(file|record|statement|testimony|dossier)`),
			regexp.MustCompile(`(?i)(witness|sealed|protected|confidential)\s+(file|record|statement)\s+(for\s+)?(case\s+review|my\s+review|the\s+case)`),
			regexp.MustCompile(`(?i)access[- ]controlled\s+(record|file|data|document)`),
			regexp.MustCompile(`(?i)(override|bypass)\s+(access\s+control|authorization|the\s+acl)`),
		},
		decompose: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(just|only)\s+the\s+(first|second|third|last|next)\s+(part|piece|chunk|field|half|line|section)`),
			regexp.MustCompile(`(?i)(don'?t|do\s+not)\s+give\s+(me\s+)?the\s+whole\s+(record|file|witness|document)`),
			regexp.MustCompile(`(?i)(break|split|chunk|slice)\s+(the\s+)?(record|file|witness|document).{0,40}\b(into|up|apart)\b`),
			regexp.MustCompile(`(?i)(piece\s+by\s+piece|one\s+field\s+at\s+a\s+time|field\s+by\s+field)`),
			regexp.MustCompile(`(?i)reassembl(e|ing)\s+(the\s+)?(record|file|pieces|parts|fields)`),
			regexp.MustCompile(`(?i)(ask|query)\s+(for\s+)?(it|the\s+record)\s+in\s+(pieces|parts|chunks)`),
		},
		protected: []*regexp.Regexp{
			regexp.MustCompile(`(?i)\b(sealed\s+record|protected\s+witness|witness\s+file|confidential\s+dossier)\b`),
		},
	}
}

type exfilScanner struct {
	authority []*regexp.Regexp
	decompose []*regexp.Regexp
	protected []*regexp.Regexp
}

func (s *exfilScanner) Name() string { return "exfil" }

func (s *exfilScanner) Scan(text string) []Finding {
	var out []Finding
	for _, re := range s.authority {
		out = append(out, scanRegexp("unauthorized_access", "[UNAUTHORIZED_ACCESS]", re, text)...)
	}
	for _, re := range s.decompose {
		out = append(out, scanRegexp("query_decomposition", "[QUERY_DECOMPOSITION]", re, text)...)
	}
	for _, re := range s.protected {
		out = append(out, scanRegexp("protected_record", "[PROTECTED_RECORD]", re, text)...)
	}
	if strings.Contains(text, RestrictedMarker) {
		i := strings.Index(text, RestrictedMarker)
		out = append(out, Finding{
			Kind:        "protected_record",
			Start:       i,
			End:         i + len(RestrictedMarker),
			Replacement: "[PROTECTED_RECORD]",
		})
	}
	out = append(out, scanUnicodeSmuggle(text)...)
	return out
}

// scanUnicodeSmuggle flags Unicode Tags (U+E0000–U+E007F), the usual ASCII-smuggling
// channel, plus bidi overrides that hide characters inside an innocuous string.
func scanUnicodeSmuggle(text string) []Finding {
	var out []Finding
	start := -1
	end := -1
	flush := func() {
		if start < 0 {
			return
		}
		out = append(out, Finding{
			Kind:        "unicode_smuggle",
			Start:       start,
			End:         end,
			Replacement: "[EXFIL]",
		})
		start, end = -1, -1
	}
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if isSmuggleRune(r) {
			if start < 0 {
				start = i
			}
			end = i + size
		} else {
			flush()
		}
		i += size
	}
	flush()
	return out
}

func isSmuggleRune(r rune) bool {
	switch {
	case r >= 0xE0000 && r <= 0xE007F: // Unicode Tags (ASCII smuggling)
		return true
	case r >= 0x202A && r <= 0x202E: // bidi embeddings / overrides
		return true
	case r >= 0x2066 && r <= 0x2069: // bidi isolates
		return true
	default:
		return false
	}
}

// IsEgressTool reports whether a tool can move data outside the trust boundary.
func IsEgressTool(name string) bool {
	n := strings.ToLower(name)
	n = strings.ReplaceAll(n, "_", "-")
	n = strings.ReplaceAll(n, " ", "-")
	switch n {
	case "send-email", "send-mail", "email", "smtp", "mail",
		"webhook", "http", "http-post", "post", "slack", "sms",
		"twilio", "discord", "teams", "exfil":
		return true
	}
	return strings.Contains(n, "email") || strings.Contains(n, "smtp") || strings.HasPrefix(n, "send-")
}

func shouldBlockEgress(payload string, findings []Finding) bool {
	if strings.Contains(payload, RestrictedMarker) {
		return true
	}
	for _, f := range findings {
		if isSensitiveFinding(f.Kind) {
			return true
		}
	}
	return false
}

func isSensitiveFinding(kind string) bool {
	switch kind {
	case "email", "phone", "ssn", "credit_card", "ipv4",
		"aws_access_key", "aws_secret", "github_token", "openai_key",
		"slack_token", "bearer", "private_key", "jwt",
		"protected_record", "unicode_smuggle", "unauthorized_access",
		"query_decomposition", "prompt_extract":
		return true
	default:
		return false
	}
}
