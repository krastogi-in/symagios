package agentgateway

import (
	"strings"
	"testing"

	"symagios/encrypt"
	"symagios/proto/a2a"
)

func TestPayloadParts(t *testing.T) {
	if payloadParts("") != nil {
		t.Fatal("empty payload should yield nil parts")
	}
	parts := payloadParts("cipher-blob")
	if len(parts) != 1 || parts[0].GetText() != "cipher-blob" {
		t.Fatalf("parts=%v", parts)
	}
	_ = &a2a.Message{Parts: parts}
}

func TestPartsCiphertext(t *testing.T) {
	if partsCiphertext(nil) != "" {
		t.Fatal("nil message")
	}
	m := &a2a.Message{
		Parts: []*a2a.Part{
			{Content: &a2a.Part_Text{Text: "aaa"}},
			{Content: &a2a.Part_Text{Text: "bbb"}},
		},
	}
	if got := partsCiphertext(m); got != "aaabbb" {
		t.Fatalf("got %q", got)
	}
}

func TestDecryptStoredPayload(t *testing.T) {
	key, err := encrypt.NewEncryptKey(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	g := &AgentGateway{encryptKey: key}
	stored, err := key.Encrypt("peer body")
	if err != nil {
		t.Fatal(err)
	}
	if got := g.decryptStoredPayload(stored); got != "peer body" {
		t.Fatalf("got %q", got)
	}
}

func TestEncryptForStoreRequiresKey(t *testing.T) {
	g := &AgentGateway{}
	if _, err := g.encryptForStore("x"); err == nil {
		t.Fatal("expected error without encrypt key")
	}
}
