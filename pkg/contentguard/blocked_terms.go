package contentguard

import (
	"regexp"
	"strings"
)

// NewBlockedTermsScanner matches configured literal/phrase denylist entries.
func NewBlockedTermsScanner(terms []string) Scanner {
	s := &blockedTermsScanner{}
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		s.patterns = append(s.patterns, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(t)))
	}
	return s
}

type blockedTermsScanner struct {
	patterns []*regexp.Regexp
}

func (s *blockedTermsScanner) Name() string { return "blocked_terms" }

func (s *blockedTermsScanner) Scan(text string) []Finding {
	var out []Finding
	for _, re := range s.patterns {
		out = append(out, scanRegexp("blocked_term", "[BLOCKED]", re, text)...)
	}
	return out
}
