package config

import (
	"flag"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDBPath       = "symagios.db"
	defaultWorkers      = 4
	defaultLogLevel     = "info"
	defaultLogFormat    = "json"
	defaultPollInterval = 2 * time.Second
)

// Shared holds settings used by engine and CLI commands.
type Shared struct {
	DBPath    string
	LogLevel  string
	LogFormat string
}

// Engine holds engine-specific settings.
type Engine struct {
	Shared
	Workers int
	// Process guards (Landlock + seccomp) for agent / skill / subagent / script sandboxes.
	GuardLandlock   bool
	GuardSeccomp    bool
	GuardAllowPaths []string
	// Content guards around LLM I/O (PII, secrets, injection).
	ContentGuard        bool
	ContentGuardMode    string // redact | block
	ContentMaxRunes     int
	ContentBlockedTerms []string
}

// Watch holds CLI watch settings.
type Watch struct {
	Shared
	PollInterval time.Duration
	SessionID    string // optional filter; empty = all sessions
	MaxPreview   int    // max runes of result/payload to print per change
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// SharedFromEnv loads common settings from environment variables.
func SharedFromEnv() Shared {
	return Shared{
		DBPath:    env("SYMAGIOS_DB_PATH", defaultDBPath),
		LogLevel:  env("SYMAGIOS_LOG_LEVEL", defaultLogLevel),
		LogFormat: env("SYMAGIOS_LOG_FORMAT", defaultLogFormat),
	}
}

// EngineFromEnv loads engine settings from environment variables.
func EngineFromEnv() Engine {
	shared := SharedFromEnv()
	return Engine{
		Shared:              shared,
		Workers:             envInt("SYMAGIOS_WORKERS", defaultWorkers),
		GuardLandlock:       env("SYMAGIOS_GUARD_LANDLOCK", "true") != "false",
		GuardSeccomp:        env("SYMAGIOS_GUARD_SECCOMP", "true") != "false",
		GuardAllowPaths:     splitCSV(env("SYMAGIOS_GUARD_ALLOW_PATHS", "/tmp/symagios-workspace")),
		ContentGuard:        env("SYMAGIOS_CONTENT_GUARD", "true") != "false",
		ContentGuardMode:    env("SYMAGIOS_CONTENT_MODE", "redact"),
		ContentMaxRunes:     envInt("SYMAGIOS_CONTENT_MAX_RUNES", 200000),
		ContentBlockedTerms: splitCSV(env("SYMAGIOS_CONTENT_BLOCKED_TERMS", "")),
	}
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// RegisterEngineFlags registers engine flags on the given flag set.
func RegisterEngineFlags(fs *flag.FlagSet, cfg *Engine) {
	fs.StringVar(&cfg.DBPath, "db", cfg.DBPath, "path to SQLite database file")
	fs.IntVar(&cfg.Workers, "workers", cfg.Workers, "number of worker goroutines")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error")
	fs.StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "log format: json or text")
	fs.BoolVar(&cfg.GuardLandlock, "guard-landlock", cfg.GuardLandlock, "apply Landlock in sandboxed agent/skill/script work")
	fs.BoolVar(&cfg.GuardSeccomp, "guard-seccomp", cfg.GuardSeccomp, "apply seccomp in sandboxed agent/skill/script work")
	fs.BoolVar(&cfg.ContentGuard, "content-guard", cfg.ContentGuard, "PII/secrets guards on pre/post LLM text")
	fs.StringVar(&cfg.ContentGuardMode, "content-mode", cfg.ContentGuardMode, "content guard mode: redact or block")
}

// RegisterWatchFlags registers watch flags on the given flag set.
func RegisterWatchFlags(fs *flag.FlagSet, cfg *Watch) {
	fs.StringVar(&cfg.DBPath, "db", cfg.DBPath, "path to SQLite database file")
	fs.DurationVar(&cfg.PollInterval, "poll", cfg.PollInterval, "poll interval for action_runs / human-input")
	fs.StringVar(&cfg.SessionID, "session", cfg.SessionID, "only show this session_id (default: all)")
	fs.IntVar(&cfg.MaxPreview, "max-preview", cfg.MaxPreview, "max runes of result/payload to print per change")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error")
	fs.StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "log format: json or text")
}

// DefaultWatch returns default watch settings.
func DefaultWatch() Watch {
	shared := SharedFromEnv()
	return Watch{
		Shared:       shared,
		PollInterval: defaultPollInterval,
		SessionID:    env("SYMAGIOS_WATCH_SESSION", ""),
		MaxPreview:   envInt("SYMAGIOS_WATCH_MAX_PREVIEW", 400),
	}
}

// DefaultLLMConfigPath is the LLM YAML config, overridable via SYMAGIOS_CONFIG.
func DefaultLLMConfigPath() string {
	if v := os.Getenv("SYMAGIOS_CONFIG"); v != "" {
		return v
	}
	const containerPath = "/etc/symagios/config.yaml"
	if _, err := os.Stat(containerPath); err == nil {
		return containerPath
	}
	return "deploy/llm/config.yaml"
}
