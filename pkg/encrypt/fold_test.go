package encrypt_test

import (
	"strings"
	"testing"

	"symagios/encrypt"
)

func TestFoldResultIntoPayloadKeepsHistory(t *testing.T) {
	key, err := encrypt.NewEncryptKey(strings.Repeat("c", 32))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := key.Encrypt("prior context")
	if err != nil {
		t.Fatal(err)
	}
	result, err := key.Encrypt("peer done")
	if err != nil {
		t.Fatal(err)
	}

	folded, cleared, changed, err := encrypt.FoldResultIntoPayload(key, payload, result)
	if err != nil || !changed || cleared != "" {
		t.Fatalf("changed=%v cleared=%q err=%v", changed, cleared, err)
	}
	plain := key.DecryptStored(folded)
	if !strings.Contains(plain, "prior context") || !strings.Contains(plain, "peer done") {
		t.Fatalf("folded=%q", plain)
	}

	_, _, changed, err = encrypt.FoldResultIntoPayload(key, folded, "")
	if err != nil || changed {
		t.Fatalf("empty result should be no-op, changed=%v err=%v", changed, err)
	}
}
