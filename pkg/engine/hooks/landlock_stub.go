//go:build !linux

package hooks

import "fmt"

type LandlockAccess uint64

const (
	LandlockRead LandlockAccess = 1 << iota
	LandlockWrite
	LandlockExecute
	LandlockReadDir
	DefaultLandlockAccess = LandlockRead | LandlockWrite | LandlockExecute | LandlockReadDir
)

// ApplyLandlock is unavailable off Linux.
func ApplyLandlock(allowPaths []string, access LandlockAccess) error {
	return fmt.Errorf("landlock: only supported on linux")
}
