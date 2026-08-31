// Package hooks wires engine lifecycle to eBPF LSM policy.
package hooks

import (
	"fmt"

	"symagios/ebpf"
	applog "symagios/log"
	"symagios/utils/types"
)

var (
	lsmController    *ebpf.LSMController
	policyController *ebpf.PolicyController
)

// InitLSM loads LSM programs, attaches hooks, and populates denied path prefixes.
func InitLSM() error {
	lsm, err := ebpf.NewLSMController()
	if err != nil {
		return fmt.Errorf("create LSM controller: %w", err)
	}

	denied := ebpf.NewDeniedPathsController(lsm)
	if err := denied.LoadDefaults(); err != nil {
		lsm.Close()
		return fmt.Errorf("load denied paths: %w", err)
	}

	lsmController = lsm
	policyController = ebpf.NewPolicyController(lsm)
	return nil
}

// CloseLSM releases LSM programs and maps.
func CloseLSM() {
	Close()
}

// Close releases LSM programs and maps.
func Close() {
	if lsmController != nil {
		if err := lsmController.Close(); err != nil {
			applog.Default().With("component", "engine").Error("failed to close LSM controller", "error", err)
		}
		lsmController = nil
		policyController = nil
	}
}

// permsForActionType returns syscall permissions for the worker cgroup during a task.
// Agent/Skill/SubAgent may call MCP/search (net) and script (exec) while WaitingFor*.
func permsForActionType(actionType types.ActionType) uint8 {
	switch actionType {
	case types.Agent, types.Skill, types.SubAgent:
		return ebpf.PermNet | ebpf.PermExec
	default:
		return 0
	}
}

// AuthorizeWorker grants syscall permissions for a worker cgroup while a task runs.
func AuthorizeWorker(cgroupID uint64, actionType types.ActionType) error {
	if policyController == nil || cgroupID == 0 {
		return nil
	}
	perms := permsForActionType(actionType)
	if perms == 0 {
		return nil
	}
	return policyController.Authorize(cgroupID, perms)
}

// RevokeWorker removes syscall permissions for a worker cgroup after a task finishes.
func RevokeWorker(cgroupID uint64) error {
	if policyController == nil || cgroupID == 0 {
		return nil
	}
	return policyController.Revoke(cgroupID)
}
