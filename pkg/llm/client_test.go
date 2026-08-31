package llm_test

import (
	"os"
	"path/filepath"
	"testing"

	"symagios/llm"
)

func TestConfigFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := []byte(`
llm:
  provider: claude
  model: claude-test
  base_url: https://example.invalid
  max_tokens: 1024
  vault_path: symagios/llm
  vault_field: api_key
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := llm.ConfigFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != llm.ProviderClaude || cfg.Model != "claude-test" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.BaseURL != "https://example.invalid" || cfg.MaxTokens != 1024 {
		t.Fatalf("%+v", cfg)
	}
	if cfg.APIKey != "" {
		t.Fatalf("yaml should not invent api key, got %q", cfg.APIKey)
	}
}

func TestConfigDefaultsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("llm:\n  provider: claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := llm.ConfigFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model == "" || cfg.BaseURL == "" || cfg.MaxTokens != 4096 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.VaultPath != "symagios/llm" || cfg.VaultField != "api_key" {
		t.Fatalf("vault defaults: %+v", cfg)
	}
}

func TestConfigFromFileAsWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("llm:\n  provider: gemini\n  model: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := llm.ConfigFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "gemini" || cfg.Model != "x" {
		t.Fatalf("%+v", cfg)
	}
}

func TestConfigClaudeVertexDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	body := []byte(`
llm:
  provider: claude-vertex
  project: demo-proj
  location: us-east5
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := llm.ConfigFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != llm.ProviderClaudeVertex {
		t.Fatalf("provider=%q", cfg.Provider)
	}
	if cfg.Model != "claude-sonnet-4@20250514" {
		t.Fatalf("model=%q", cfg.Model)
	}
	if cfg.VaultField != "access_token" {
		t.Fatalf("vault_field=%q", cfg.VaultField)
	}
	if cfg.Project != "demo-proj" || cfg.Location != "us-east5" {
		t.Fatalf("%+v", cfg)
	}
}
