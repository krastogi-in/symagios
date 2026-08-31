package ebpf

import (
	"fmt"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
)

// LSMController owns the loaded eBPF programs/maps and their kernel
// attachments. Create exactly one per process, at engine startup, and
// Close() it on shutdown.
type LSMController struct {
	objs  PreHookObjects
	links []link.Link
}

func NewLSMController() (*LSMController, error) {
	// Rootless Podman often cannot raise RLIMIT_MEMLOCK even with CAP_SYS_RESOURCE.
	// Kernels that account BPF memory via memcg still load maps without unlimited memlock.
	if err := rlimit.RemoveMemlock(); err != nil {
		// Continue; LoadPreHookObjects will fail clearly if memlock is truly insufficient.
		_ = err
	}

	var objs PreHookObjects
	if err := LoadPreHookObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("loading eBPF objects (verifier rejected, or BTF/LSM support missing): %w", err)
	}

	c := &LSMController{objs: objs}

	execLink, err := link.AttachLSM(link.LSMOptions{Program: objs.CheckExec})
	if err != nil {
		objs.Close()
		return nil, fmt.Errorf("attaching exec LSM hook (check /sys/kernel/security/lsm includes 'bpf'): %w", err)
	}
	c.links = append(c.links, execLink)

	fileLink, err := link.AttachLSM(link.LSMOptions{Program: objs.CheckFileOpen})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("attaching file_open LSM hook: %w", err)
	}
	c.links = append(c.links, fileLink)

	netLink, err := link.AttachLSM(link.LSMOptions{Program: objs.CheckSocketConnect})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("attaching socket_connect LSM hook: %w", err)
	}
	c.links = append(c.links, netLink)

	return c, nil
}

func (c *LSMController) Close() error {
	for _, l := range c.links {
		l.Close()
	}
	return c.objs.Close()
}