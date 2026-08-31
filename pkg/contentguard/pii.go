package contentguard

import "regexp"

// PII scanner: email, phone, SSN, credit-card-shaped numbers, IPv4.
type piiScanner struct {
	email *regexp.Regexp
	phone *regexp.Regexp
	ssn   *regexp.Regexp
	cc    *regexp.Regexp
	ipv4  *regexp.Regexp
}

// NewPIIScanner returns common PII detectors.
func NewPIIScanner() Scanner {
	return &piiScanner{
		email: regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`),
		phone: regexp.MustCompile(`(?i)(?:\+?\d{1,3}[\s\-.]*)?(?:\(?\d{3}\)?[\s\-.]*)\d{3}[\s\-.]*\d{4}\b`),
		ssn:   regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		cc:    regexp.MustCompile(`\b(?:\d[ \-]*){13,19}\b`),
		ipv4:  regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`),
	}
}

func (s *piiScanner) Name() string { return "pii" }

func (s *piiScanner) Scan(text string) []Finding {
	var out []Finding
	out = append(out, scanRegexp("email", "[EMAIL]", s.email, text)...)
	out = append(out, scanRegexp("phone", "[PHONE]", s.phone, text)...)
	out = append(out, scanRegexp("ssn", "[SSN]", s.ssn, text)...)
	out = append(out, scanRegexp("ipv4", "[IP]", s.ipv4, text)...)
	for _, f := range scanRegexp("credit_card", "[CARD]", s.cc, text) {
		digits := digitsOnly(text[f.Start:f.End])
		if len(digits) < 13 || len(digits) > 19 || !luhnOK(digits) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func digitsOnly(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b = append(b, s[i])
		}
	}
	return string(b)
}

func luhnOK(digits string) bool {
	sum := 0
	alt := false
	for i := len(digits) - 1; i >= 0; i-- {
		n := int(digits[i] - '0')
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0
}
