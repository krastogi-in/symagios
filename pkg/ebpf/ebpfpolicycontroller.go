package ebpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Must match the #define values in ebpf/prehook.c exactly.
const (
	PermExec uint8 = 1 << 0
	PermFile uint8 = 1 << 1
	PermNet  uint8 = 1 << 2
)

type PolicyController struct {
	policyMap *ebpf.Map
}

func NewPolicyController(c *LSMController) *PolicyController {
	return &PolicyController{policyMap: c.objs.PolicyMap}
}

// Authorize sets the FULL permission set for a worker's cgroup, replacing
// whatever was there before. Call this right before a task runs, with only
// the permissions that task actually needs.
func (p *PolicyController) Authorize(cgroupID uint64, perms uint8) error {
	if err := p.policyMap.Update(&cgroupID, &perms, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("authorizing cgroup %d for perms 0x%02x: %w", cgroupID, perms, err)
	}
	return nil
}

// Narrow reduces permissions to exactly `perms` (same as Authorize - kept
// as a distinct name so call sites read clearly: widen before risky work,
// narrow immediately after).
func (p *PolicyController) Narrow(cgroupID uint64, perms uint8) error {
	return p.Authorize(cgroupID, perms)
}

// Revoke removes all permissions for a worker's cgroup - the safe default
// state between tasks. A missing map entry means "everything denied," per
// the eBPF program's lookup_perm() returning 0 on a miss.
func (p *PolicyController) Revoke(cgroupID uint64) error {
	if err := p.policyMap.Delete(&cgroupID); err != nil {
		return fmt.Errorf("revoking policy for cgroup %d: %w", cgroupID, err)
	}
	return nil
}