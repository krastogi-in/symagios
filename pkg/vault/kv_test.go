package vault_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"symagios/vault"
)

func TestGetField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "tok" {
			http.Error(w, "unauth", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"data": map[string]string{"api_key": "secret-value"},
			},
		})
	}))
	defer srv.Close()

	val, err := vault.GetField(context.Background(), vault.Config{
		Addr:  srv.URL,
		Token: "tok",
		Path:  "symagios/llm",
	}, "api_key")
	if err != nil {
		t.Fatal(err)
	}
	if val != "secret-value" {
		t.Fatalf("got %q", val)
	}
}

func TestGetFieldMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"data": map[string]string{}},
		})
	}))
	defer srv.Close()

	_, err := vault.GetField(context.Background(), vault.Config{
		Addr:  srv.URL,
		Token: "tok",
		Path:  "symagios/llm",
	}, "api_key")
	if err == nil {
		t.Fatal("expected missing field error")
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("VAULT_ADDR", "https://vault.test:8201")
	t.Setenv("VAULT_TOKEN", "root-test")
	t.Setenv("SYMAGIOS_VAULT_ENGINE_PATH", "symagios/custom")
	cfg := vault.ConfigFromEnv()
	if cfg.Addr != "https://vault.test:8201" || cfg.Token != "root-test" || cfg.Path != "symagios/custom" {
		t.Fatalf("%+v", cfg)
	}
}
