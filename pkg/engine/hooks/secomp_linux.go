//go:build linux

package hooks

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ApplySeccomp installs a filter that kills the process on denied syscalls.
// Inherited by children after exec — apply in the script-sandbox shim only.
func ApplySeccomp(denySyscalls []int) error {
	if len(denySyscalls) == 0 {
		denySyscalls = defaultDeniedSyscalls()
	}

	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("seccomp PR_SET_NO_NEW_PRIVS: %w", err)
	}

	type seccompData struct {
		Nr                 int32
		Arch               uint32
		InstructionPointer uint64
		Args               [6]uint64
	}
	var data seccompData

	var expectedArch uint32
	switch runtime.GOARCH {
	case "amd64":
		expectedArch = unix.AUDIT_ARCH_X86_64
	case "arm64":
		expectedArch = unix.AUDIT_ARCH_AARCH64
	default:
		return fmt.Errorf("seccomp: unsupported GOARCH %s", runtime.GOARCH)
	}

	archOff := uint32(unsafe.Offsetof(data.Arch))
	nrOff := uint32(unsafe.Offsetof(data.Nr))

	// Layout (from go_learning/test_secomp.go, generalized):
	// 0 load arch
	// 1 jeq expectedArch else kill
	// 2 load nr
	// then one jeq-kill per denied syscall
	// allow
	// kill
	nDeny := len(denySyscalls)
	sf := make([]unix.SockFilter, 0, 4+nDeny)
	sf = append(sf,
		unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: archOff},
		// Next insn is 2; kill sits at 4+nDeny → Jf = 2+nDeny.
		unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: expectedArch, Jt: 0, Jf: uint8(2 + nDeny)},
		unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: nrOff},
	)
	for i, nr := range denySyscalls {
		// Jump to kill at 4+nDeny: from insn 3+i, jt = nDeny-i.
		jt := uint8(nDeny - i)
		sf = append(sf, unix.SockFilter{
			Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K,
			K:    uint32(nr),
			Jt:   jt,
			Jf:   0,
		})
	}
	sf = append(sf,
		unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
	)

	prog := unix.SockFprog{
		Len:    uint16(len(sf)),
		Filter: &sf[0],
	}
	if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&prog)), 0, 0); err != nil {
		return fmt.Errorf("seccomp PR_SET_SECCOMP: %w", err)
	}
	return nil
}

func defaultDeniedSyscalls() []int {
	return []int{
		unix.SYS_MOUNT,
		unix.SYS_UMOUNT2,
		unix.SYS_REBOOT,
	}
}
