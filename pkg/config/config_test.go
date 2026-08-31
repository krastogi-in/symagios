package config_test

import (
	"testing"

	"symagios/config"
)

func TestEngineFromEnvDefaults(t *testing.T) {
	t.Setenv("SYMAGIOS_DB_PATH", "")
	t.Setenv("SYMAGIOS_WORKERS", "")
	t.Setenv("SYMAGIOS_CONTENT_GUARD", "")
	cfg := config.EngineFromEnv()
	if cfg.Workers != 4 {
		t.Fatalf("workers=%d", cfg.Workers)
	}
	if !cfg.ContentGuard {
		t.Fatal("content guard should default on")
	}
	if cfg.ContentGuardMode != "redact" {
		t.Fatalf("mode=%s", cfg.ContentGuardMode)
	}
}

func TestEngineFromEnvOverrides(t *testing.T) {
	t.Setenv("SYMAGIOS_DB_PATH", "/tmp/t.db")
	t.Setenv("SYMAGIOS_WORKERS", "8")
	t.Setenv("SYMAGIOS_CONTENT_MODE", "block")
	t.Setenv("SYMAGIOS_CONTENT_BLOCKED_TERMS", "alpha, beta ,")
	t.Setenv("SYMAGIOS_GUARD_LANDLOCK", "false")
	cfg := config.EngineFromEnv()
	if cfg.DBPath != "/tmp/t.db" || cfg.Workers != 8 {
		t.Fatalf("%+v", cfg)
	}
	if cfg.ContentGuardMode != "block" {
		t.Fatalf("mode=%s", cfg.ContentGuardMode)
	}
	if cfg.GuardLandlock {
		t.Fatal("landlock should be off")
	}
	if len(cfg.ContentBlockedTerms) != 2 || cfg.ContentBlockedTerms[0] != "alpha" {
		t.Fatalf("terms=%v", cfg.ContentBlockedTerms)
	}
}
