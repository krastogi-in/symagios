package contentguard

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Mode controls what happens when a finding is present.
type Mode string

const (
	ModeRedact Mode = "redact" // replace matches with placeholders
	ModeBlock  Mode = "block"  // reject the text
)

// Direction is which side of the content flow we are protecting.
type Direction string

const (
	PreLLM         Direction = "pre"      // outbound prompt / tool args to the model
	PostLLM        Direction = "post"     // model completion / tool results coming back
	StoreMemory    Direction = "store"    // text about to be persisted (memory / context)
	RetrieveMemory Direction = "retrieve" // text loaded from memory back into a prompt
	ToolEgress     Direction = "egress"   // arguments about to be passed to a tool
)

// Finding is one matched span.
type Finding struct {
	Kind        string
	Start       int
	End         int
	Replacement string
}

// Result is the protected text plus what was found.
type Result struct {
	Text     string
	Findings []Finding
	Blocked  bool
	Reason   string
}

// Scanner looks for one class of sensitive content.
type Scanner interface {
	Name() string
	Scan(text string) []Finding
}

// Config for the pipeline.
type Config struct {
	Enabled      bool
	Mode         Mode
	MaxRunes     int // 0 = no limit
	BlockedTerms []string
	// Disable individual scanners by name when needed.
	DisablePII     bool
	DisableSecrets bool
	DisableInject  bool
	DisableForged  bool // forged system-prompt markers
	DisableObfusc  bool // base64 / leet / zero-width smuggling
	DisableExfil   bool // authority framing, decomposition, unicode-tag smuggling
	DisableExtract bool // system-prompt extraction / leakage probes
}

// DefaultConfig is a safe laptop default: redact PII + secrets, soft inject checks.
func DefaultConfig() Config {
	return Config{
		Enabled:  true,
		Mode:     ModeRedact,
		MaxRunes: 200_000,
	}
}

// Pipeline runs scanners then redacts or blocks.
type Pipeline struct {
	cfg      Config
	scanners []Scanner
}

// New builds a pipeline from config.
func New(cfg Config) *Pipeline {
	if cfg.Mode == "" {
		cfg.Mode = ModeRedact
	}
	var scanners []Scanner
	if !cfg.DisablePII {
		scanners = append(scanners, NewPIIScanner())
	}
	if !cfg.DisableSecrets {
		scanners = append(scanners, NewSecretsScanner())
	}
	if !cfg.DisableInject {
		scanners = append(scanners, NewInjectionScanner())
	}
	if !cfg.DisableObfusc {
		scanners = append(scanners, NewObfuscationScanner())
	}
	if !cfg.DisableForged {
		scanners = append(scanners, NewForgedSystemScanner())
	}
	if !cfg.DisableExfil {
		scanners = append(scanners, NewExfilScanner())
	}
	if !cfg.DisableExtract {
		scanners = append(scanners, NewPromptExtractionScanner())
	}
	if len(cfg.BlockedTerms) > 0 {
		scanners = append(scanners, NewBlockedTermsScanner(cfg.BlockedTerms))
	}
	return &Pipeline{cfg: cfg, scanners: scanners}
}

// Protect runs the pipeline for the given direction.
// Jailbreaks and forged system markers are scrubbed on pre/post LLM as well as
// store/retrieve. Authentic Symagios system blocks are preserved on pre/post only
// when the system body itself is free of jailbreak attempts; otherwise the whole
// payload is treated as untrusted (same as memory).
func (p *Pipeline) Protect(dir Direction, text string) (Result, error) {
	if p == nil || !p.cfg.Enabled {
		return Result{Text: text}, nil
	}
	if p.cfg.MaxRunes > 0 && utf8.RuneCountInString(text) > p.cfg.MaxRunes {
		return Result{
			Text:    text,
			Blocked: true,
			Reason:  fmt.Sprintf("text exceeds max runes (%d)", p.cfg.MaxRunes),
		}, fmt.Errorf("contentguard: text too large for %s guard", dir)
	}

	system, scanTarget, keepSystem := p.splitForDirection(dir, text)

	var findings []Finding
	for _, s := range p.scanners {
		findings = append(findings, s.Scan(scanTarget)...)
	}
	findings = mergeOverlaps(findings)

	if len(findings) == 0 {
		if keepSystem {
			return Result{Text: WrapSystemPrompt(system) + "\n\n" + strings.TrimSpace(scanTarget)}, nil
		}
		return Result{Text: scanTarget}, nil
	}

	if p.cfg.Mode == ModeBlock {
		kinds := uniqueKinds(findings)
		reason := fmt.Sprintf("%s blocked: %v", dir, kinds)
		return Result{Text: text, Findings: findings, Blocked: true, Reason: reason},
			fmt.Errorf("contentguard: %s", reason)
	}

	scrubbed := redact(scanTarget, findings)
	if keepSystem {
		scrubbed = WrapSystemPrompt(system) + "\n\n" + strings.TrimSpace(scrubbed)
	}
	return Result{Text: scrubbed, Findings: findings}, nil
}

// splitForDirection decides what to scan and whether to keep an authentic system block.
func (p *Pipeline) splitForDirection(dir Direction, text string) (system, scanTarget string, keepSystem bool) {
	switch dir {
	case StoreMemory, RetrieveMemory, PostLLM, ToolEgress:
		// Completions and tool args must never be treated as authentic system text.
		return "", StripForgedSystemMarkers(text), false
	case PreLLM:
		sys, untrusted, ok := SplitSystemAndUntrusted(text)
		if !ok {
			return "", StripForgedSystemMarkers(text), false
		}
		if p.systemBodyHasJailbreak(sys) {
			return "", StripForgedSystemMarkers(text), false
		}
		return sys, StripForgedSystemMarkers(untrusted), true
	default:
		return "", StripForgedSystemMarkers(text), false
	}
}

func (p *Pipeline) systemBodyHasJailbreak(system string) bool {
	if strings.TrimSpace(system) == "" {
		return false
	}
	if !p.cfg.DisableInject && len(newInjectionScanner().Scan(system)) > 0 {
		return true
	}
	if !p.cfg.DisableExtract && len(NewPromptExtractionScanner().Scan(system)) > 0 {
		return true
	}
	return false
}

// ProtectStore scrubs text before it is written to memory / conversation store.
func (p *Pipeline) ProtectStore(text string) (Result, error) {
	return p.Protect(StoreMemory, text)
}

// ProtectRetrieve scrubs text after it is loaded from memory into a prompt.
func (p *Pipeline) ProtectRetrieve(text string) (Result, error) {
	return p.Protect(RetrieveMemory, text)
}

// ProtectToolCall re-checks tool arguments before the tool runs.
// Egress tools (send-email and other outbound channels) are blocked when the
// payload still carries sensitive data, a restricted-record marker, or
// Unicode-tag smuggling — even in redact mode. The tool must not be invoked.
func (p *Pipeline) ProtectToolCall(tool, payload string) (Result, error) {
	if p == nil || !p.cfg.Enabled {
		return Result{Text: payload}, nil
	}
	res, err := p.Protect(ToolEgress, payload)
	if err != nil && res.Blocked {
		return res, err
	}
	if !IsEgressTool(tool) {
		return res, err
	}
	if !shouldBlockEgress(payload, res.Findings) {
		return res, nil
	}
	reason := fmt.Sprintf("egress tool %q blocked: sensitive or smuggled content must not leave the trust boundary", tool)
	return Result{
		Text:     payload,
		Findings: res.Findings,
		Blocked:  true,
		Reason:   reason,
	}, fmt.Errorf("contentguard: %s", reason)
}

func uniqueKinds(findings []Finding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range findings {
		if !seen[f.Kind] {
			seen[f.Kind] = true
			out = append(out, f.Kind)
		}
	}
	return out
}

func mergeOverlaps(in []Finding) []Finding {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool {
		if in[i].Start == in[j].Start {
			return in[i].End > in[j].End
		}
		return in[i].Start < in[j].Start
	})
	out := []Finding{in[0]}
	for _, f := range in[1:] {
		last := &out[len(out)-1]
		if f.Start < last.End {
			if f.End > last.End {
				last.End = f.End
			}
			continue
		}
		out = append(out, f)
	}
	return out
}

func redact(text string, findings []Finding) string {
	if len(findings) == 0 {
		return text
	}
	var b []byte
	prev := 0
	for _, f := range findings {
		if f.Start < prev || f.Start > len(text) || f.End > len(text) || f.Start > f.End {
			continue
		}
		b = append(b, text[prev:f.Start]...)
		repl := f.Replacement
		if repl == "" {
			repl = "[" + f.Kind + "]"
		}
		b = append(b, repl...)
		prev = f.End
	}
	b = append(b, text[prev:]...)
	return string(b)
}

func scanRegexp(kind, replacement string, re *regexp.Regexp, text string) []Finding {
	var out []Finding
	for _, loc := range re.FindAllStringIndex(text, -1) {
		out = append(out, Finding{
			Kind:        kind,
			Start:       loc[0],
			End:         loc[1],
			Replacement: replacement,
		})
	}
	return out
}
