package contentguard_test

import (
	"strings"
	"testing"

	"symagios/contentguard"
)

func TestBlockedTerms(t *testing.T) {
	p := contentguard.New(contentguard.Config{
		Enabled:      true,
		Mode:         contentguard.ModeRedact,
		BlockedTerms: []string{"classified-project"},
	})
	out, err := p.Protect(contentguard.PreLLM, "discuss classified-project today")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "[BLOCKED]") {
		t.Fatalf("%s", out.Text)
	}
}

func TestToolEgressDirection(t *testing.T) {
	p := contentguard.New(contentguard.Config{Enabled: true, Mode: contentguard.ModeRedact})
	res, err := p.ProtectToolCall("send-email", "contact +1-415-555-0100")
	if err == nil || !res.Blocked {
		t.Fatalf("expected egress block: %#v %v", res, err)
	}
}
