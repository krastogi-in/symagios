//go:build linux

package hooks

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// LandlockAccess is a filesystem access mask for Landlock rules.
type LandlockAccess uint64

const (
	LandlockRead    LandlockAccess = unix.LANDLOCK_ACCESS_FS_READ_FILE
	LandlockWrite   LandlockAccess = unix.LANDLOCK_ACCESS_FS_WRITE_FILE
	LandlockExecute LandlockAccess = unix.LANDLOCK_ACCESS_FS_EXECUTE
	LandlockReadDir LandlockAccess = unix.LANDLOCK_ACCESS_FS_READ_DIR
)

// DefaultLandlockAccess is the FS mask unless config overrides.
const DefaultLandlockAccess = LandlockRead | LandlockWrite | LandlockExecute | LandlockReadDir

// ApplyLandlock restricts the calling process to the given path trees.
// Inherited by children after exec — apply in the script-sandbox shim only.
func ApplyLandlock(allowPaths []string, access LandlockAccess) error {
	if len(allowPaths) == 0 {
		return fmt.Errorf("landlock: at least one allow path is required")
	}
	if access == 0 {
		access = DefaultLandlockAccess
	}

	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("landlock PR_SET_NO_NEW_PRIVS: %w", err)
	}

	attr := unix.LandlockRulesetAttr{
		Access_fs: uint64(access),
	}
	rulesetFD, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)),
		uintptr(unsafe.Sizeof(attr)),
		0,
	)
	if errno != 0 {
		return fmt.Errorf("landlock create ruleset: %w", errno)
	}
	defer unix.Close(int(rulesetFD))

	for _, path := range allowPaths {
		parentFD, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("landlock open %q: %w", path, err)
		}
		pathBeneath := unix.LandlockPathBeneathAttr{
			Allowed_access: uint64(access),
			Parent_fd:      int32(parentFD),
		}
		_, _, errno = unix.Syscall6(
			unix.SYS_LANDLOCK_ADD_RULE,
			uintptr(rulesetFD),
			uintptr(unix.LANDLOCK_RULE_PATH_BENEATH),
			uintptr(unsafe.Pointer(&pathBeneath)),
			0, 0, 0,
		)
		_ = unix.Close(parentFD)
		if errno != 0 {
			return fmt.Errorf("landlock add rule %q: %w", path, errno)
		}
	}

	_, _, errno = unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(rulesetFD), 0, 0)
	if errno != 0 {
		return fmt.Errorf("landlock restrict self: %w", errno)
	}
	return nil
}
