package contentguard

import "regexp"

// Secrets / credential patterns that should never go to or from an LLM.
type secretsScanner struct {
	patterns []namedRE
}

type namedRE struct {
	kind, repl string
	re         *regexp.Regexp
}

// NewSecretsScanner detects common API keys and private key material.
func NewSecretsScanner() Scanner {
	return &secretsScanner{
		patterns: []namedRE{
			{"aws_access_key", "[AWS_KEY]", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
			{"aws_secret", "[AWS_SECRET]", regexp.MustCompile(`(?i)\baws[_\-]?secret[_\-]?access[_\-]?key\b\s*[:=]\s*['\"]?([A-Za-z0-9/+=]{30,})`)},
			{"github_token", "[GH_TOKEN]", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`)},
			{"openai_key", "[OPENAI_KEY]", regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}\b`)},
			{"slack_token", "[SLACK_TOKEN]", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
			{"bearer", "[BEARER]", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9\-._~+/]+=*`)},
			{"private_key", "[PRIVATE_KEY]", regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`)},
			{"jwt", "[JWT]", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)},
		},
	}
}

func (s *secretsScanner) Name() string { return "secrets" }

func (s *secretsScanner) Scan(text string) []Finding {
	var out []Finding
	for _, p := range s.patterns {
		out = append(out, scanRegexp(p.kind, p.repl, p.re, text)...)
	}
	return out
}
