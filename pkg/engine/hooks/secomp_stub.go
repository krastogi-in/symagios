//go:build !linux

package hooks

import "fmt"

// ApplySeccomp is unavailable off Linux.
func ApplySeccomp(denySyscalls []int) error {
	return fmt.Errorf("seccomp: only supported on linux")
}
