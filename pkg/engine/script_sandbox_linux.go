//go:build linux

package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"symagios/engine/hooks"

	"golang.org/x/sys/unix"
)

const (
	scriptSandboxArg = "script-sandbox"
	scriptAllowEnv   = "SYMAGIOS_SCRIPT_ALLOW_PATHS"
)

func init() {
	// Re-exec entry: worker launches this process as a shim; we jail ourselves
	// then exec the interpreter so Landlock/seccomp are inherited by the script.
	if len(os.Args) >= 2 && os.Args[1] == scriptSandboxArg {
		os.Exit(runScriptSandbox(os.Args[2:]))
	}
}

func runScriptSandbox(args []string) int {
	for len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "script-sandbox: missing interpreter argv")
		return 2
	}

	cfg := hooks.Guardians()
	if raw := os.Getenv(scriptAllowEnv); raw != "" {
		if paths := hooks.ParseAllowPaths(raw); len(paths) > 0 {
			cfg.AllowPaths = paths
		}
	}

	if cfg.Landlock || cfg.Seccomp {
		if err := hooks.ApplyGuards(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "script-sandbox: apply guards: %v\n", err)
			return 1
		}
	}

	if err := unix.Exec(args[0], args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "script-sandbox: exec %s: %v\n", args[0], err)
		return 127
	}
	return 0
}

func scriptSandboxDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SYMAGIOS_SCRIPT_SANDBOX")))
	return v == "0" || v == "false" || v == "off"
}

func scriptAllowPaths(workDir, interpreter string) []string {
	cfg := hooks.Guardians()
	seen := map[string]struct{}{}
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for _, p := range cfg.AllowPaths {
		add(p)
	}
	add(workDir)
	interpDir := filepath.Dir(interpreter)
	add(interpDir)
	// venv/conda trees keep libs next to bin (…/prefix/bin/python → …/prefix).
	add(filepath.Dir(interpDir))
	// Dynamic linker + common interpreter trees (read/exec under Landlock).
	for _, p := range []string{"/usr", "/bin", "/lib", "/lib64", "/sbin"} {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			add(p)
		}
	}
	return out
}

// newScriptCmd builds either a sandboxed re-exec shim or a direct interpreter command.
func newScriptCmd(ctx context.Context, bin string, argv []string, workDir, dir string) (*exec.Cmd, error) {
	if scriptSandboxDisabled() {
		cmd := exec.CommandContext(ctx, bin, argv...)
		cmd.Dir = dir
		return cmd, nil
	}

	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("script sandbox executable: %w", err)
	}

	shimArgs := append([]string{scriptSandboxArg, "--", bin}, argv...)
	cmd := exec.CommandContext(ctx, self, shimArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), scriptAllowEnv+"="+strings.Join(scriptAllowPaths(workDir, bin), ","))
	return cmd, nil
}
