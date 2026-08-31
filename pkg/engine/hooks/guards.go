package hooks

import (
	"fmt"
	"strings"
)

// GuardConfig controls Landlock + seccomp for sandboxed action work.
// Use for agents, skills, subagents, and scripts when they run (or spawn)
// code that should not see the whole filesystem or dangerous syscalls.
type GuardConfig struct {
	// Landlock enables filesystem path jailing.
	Landlock bool
	// Seccomp enables syscall filtering (kill on deny).
	Seccomp bool
	// AllowPaths are directory trees Landlock permits.
	// Keep tight: session/workspace dirs, interpreters if needed — not "/".
	AllowPaths []string
	// LandlockAccess overrides DefaultLandlockAccess when non-zero.
	LandlockAccess LandlockAccess
	// DenySyscalls overrides the default mount/umount2/reboot set when non-empty.
	DenySyscalls []int
}

var configuredGuards = DefaultGuardConfig()

// DefaultGuardConfig enables both guards with a small workspace allow-list.
func DefaultGuardConfig() GuardConfig {
	return GuardConfig{
		Landlock: true,
		Seccomp:  true,
		AllowPaths: []string{
			"/tmp/symagios-workspace",
		},
		LandlockAccess: DefaultLandlockAccess,
	}
}

// ConfigureGuards sets process-wide defaults for ApplyGuards (script-sandbox shim).
func ConfigureGuards(cfg GuardConfig) {
	if len(cfg.AllowPaths) == 0 {
		cfg.AllowPaths = DefaultGuardConfig().AllowPaths
	}
	configuredGuards = cfg
}

// Guardians returns the current guard config.
func Guardians() GuardConfig {
	return configuredGuards
}

// ApplyGuards applies Landlock then seccomp on the calling process.
// These stick for the process lifetime and are inherited by children after
// exec — call from the script-sandbox shim, never on a long-lived worker.
func ApplyGuards(cfg GuardConfig) error {
	if !cfg.Landlock && !cfg.Seccomp {
		return nil
	}
	if cfg.Landlock {
		if err := ApplyLandlock(cfg.AllowPaths, cfg.LandlockAccess); err != nil {
			return fmt.Errorf("landlock: %w", err)
		}
	}
	if cfg.Seccomp {
		if err := ApplySeccomp(cfg.DenySyscalls); err != nil {
			return fmt.Errorf("seccomp: %w", err)
		}
	}
	return nil
}

// ParseAllowPaths splits a comma-separated env value into paths.
func ParseAllowPaths(raw string) []string {
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
