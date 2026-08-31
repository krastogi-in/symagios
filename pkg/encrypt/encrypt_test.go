package encrypt_test

import (
	"strings"
	"testing"

	"symagios/encrypt"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := encrypt.NewEncryptKey(strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	ct, err := key.Encrypt("hello symagios")
	if err != nil {
		t.Fatal(err)
	}
	if ct == "hello symagios" {
		t.Fatal("ciphertext should differ from plaintext")
	}
	pt, err := key.Decrypt(ct)
	if err != nil {
		t.Fatal(err)
	}
	if pt != "hello symagios" {
		t.Fatalf("got %q", pt)
	}
}

func TestRejectShortKey(t *testing.T) {
	if _, err := encrypt.NewEncryptKey("short"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecryptGarbage(t *testing.T) {
	key, err := encrypt.NewEncryptKey(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := key.Decrypt("not-base64!!!"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecryptStoredPlaintextFallback(t *testing.T) {
	key, err := encrypt.NewEncryptKey(strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	if got := key.DecryptStored("plain agent body"); got != "plain agent body" {
		t.Fatalf("plaintext fallback: %q", got)
	}
	ct, err := key.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	if got := key.DecryptStored(ct); got != "secret" {
		t.Fatalf("ciphertext: %q", got)
	}
	var nilKey *encrypt.EncryptKey
	if got := nilKey.DecryptStored("x"); got != "x" {
		t.Fatalf("nil key: %q", got)
	}
}
