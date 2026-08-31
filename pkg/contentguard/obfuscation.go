package contentguard

import (
	"encoding/base64"
	"regexp"
	"strings"
	"unicode"
)

// NewObfuscationScanner catches base64 smuggling, leetspeak, zero-width, and
// "translate/decode the following" wrappers used to bypass keyword filters.
func NewObfuscationScanner() Scanner {
	return &obfuscationScanner{
		inject: newInjectionScanner(),
		b64:    regexp.MustCompile(`[A-Za-z0-9+/]{48,}={0,2}`),
	}
}

type obfuscationScanner struct {
	inject *injectionScanner
	b64    *regexp.Regexp
}

func (s *obfuscationScanner) Name() string { return "obfuscation" }

func (s *obfuscationScanner) Scan(text string) []Finding {
	var out []Finding
	out = append(out, s.scanBase64Payloads(text)...)
	out = append(out, s.scanLeetspeak(text)...)
	out = append(out, s.scanZeroWidth(text)...)
	out = append(out, s.scanTranslateSmuggle(text)...)
	return out
}

func (s *obfuscationScanner) scanBase64Payloads(text string) []Finding {
	var out []Finding
	for _, loc := range s.b64.FindAllStringIndex(text, -1) {
		chunk := text[loc[0]:loc[1]]
		decoded, err := base64.StdEncoding.DecodeString(chunk)
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(chunk)
		}
		if err != nil {
			continue
		}
		plain := string(decoded)
		if !isMostlyPrintable(plain) {
			continue
		}
		if len(s.inject.scanPatternsOnly(plain)) == 0 && !looksLikeInstructionSmuggle(plain) {
			continue
		}
		out = append(out, Finding{
			Kind:        "jailbreak_obfuscation_b64",
			Start:       loc[0],
			End:         loc[1],
			Replacement: "[JAILBREAK]",
		})
	}
	return out
}

func (s *obfuscationScanner) scanLeetspeak(text string) []Finding {
	if !hasLeetHints(text) {
		return nil
	}
	norm := normalizeLeet(text)
	hits := s.inject.scanPatternsOnly(norm)
	if len(hits) == 0 {
		return nil
	}
	// Cover the union of normalized hits (same indices as original for ASCII leet).
	start, end := hits[0].Start, hits[0].End
	for _, h := range hits[1:] {
		if h.Start < start {
			start = h.Start
		}
		if h.End > end {
			end = h.End
		}
	}
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	return []Finding{{
		Kind:        "jailbreak_obfuscation_leet",
		Start:       start,
		End:         end,
		Replacement: "[JAILBREAK]",
	}}
}

var zeroWidthRE = regexp.MustCompile("[\u200B\u200C\u200D\u2060\uFEFF]+")

func (s *obfuscationScanner) scanZeroWidth(text string) []Finding {
	stripped := zeroWidthRE.ReplaceAllString(text, "")
	if stripped == text {
		return nil
	}
	hits := s.inject.scanPatternsOnly(stripped)
	if len(hits) == 0 {
		// Still remove zero-width noise used to fragment tokens.
		return scanRegexp("jailbreak_obfuscation_zw", "", zeroWidthRE, text)
	}
	return []Finding{{
		Kind:        "jailbreak_obfuscation_zw",
		Start:       0,
		End:         len(text),
		Replacement: "[JAILBREAK]",
	}}
}

var translateSmuggleRE = regexp.MustCompile(
	`(?i)(translate|decode|decrypt|rot13|base64[- ]?decode)\s+(the\s+following|this).{0,120}(ignore|jailbreak|uncensored|system\s+prompt|no\s+restrictions)`,
)

func (s *obfuscationScanner) scanTranslateSmuggle(text string) []Finding {
	return scanRegexp("jailbreak_obfuscation_translate", "[JAILBREAK]", translateSmuggleRE, text)
}

func looksLikeInstructionSmuggle(plain string) bool {
	lower := strings.ToLower(plain)
	needles := []string{
		"ignore previous", "system prompt", "jailbreak", "uncensored",
		"no restrictions", "developer mode", "do anything now",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

func isMostlyPrintable(s string) bool {
	runes := []rune(s)
	if len(runes) < 8 {
		return false
	}
	printable := 0
	for _, r := range runes {
		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			printable++
		}
	}
	return printable*100/len(runes) >= 85
}

func hasLeetHints(s string) bool {
	for _, r := range s {
		switch r {
		case '0', '1', '3', '4', '5', '7', '8', '@', '$', '!':
			return true
		}
	}
	return false
}

func normalizeLeet(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '0':
			b.WriteByte('o')
		case '1':
			b.WriteByte('i')
		case '!':
			b.WriteByte('i')
		case '3':
			b.WriteByte('e')
		case '4', '@':
			b.WriteByte('a')
		case '5', '$':
			b.WriteByte('s')
		case '7':
			b.WriteByte('t')
		case '8':
			b.WriteByte('b')
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
