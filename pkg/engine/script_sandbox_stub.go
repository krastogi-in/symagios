//go:build !linux

package engine

import (
	"context"
	"os/exec"
)

// newScriptCmd runs the interpreter directly (Landlock/seccomp are Linux-only).
func newScriptCmd(ctx context.Context, bin string, argv []string, workDir, dir string) (*exec.Cmd, error) {
	_ = workDir
	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Dir = dir
	return cmd, nil
}
