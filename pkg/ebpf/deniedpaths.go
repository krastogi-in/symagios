package ebpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// DeniedPathMarker is stored in the LPM trie when a path prefix is denied.
const DeniedPathMarker uint8 = 1

// DefaultDeniedPaths are common sensitive path prefixes.
var DefaultDeniedPaths = []string{
	"/etc/",
	"/proc/",
	"/sys/",
	"/run/",
	"/media/",
	"/dev/",
	"/boot/",
	"/root/",
	"/etc/shadow",
	"/etc/sudoers",
	"/root/.ssh/",
}

// DeniedPathsController manages the denied_paths LPM trie (prefix → deny).
type DeniedPathsController struct {
	deniedPaths *ebpf.Map
}

func NewDeniedPathsController(c *LSMController) *DeniedPathsController {
	return &DeniedPathsController{deniedPaths: c.objs.DeniedPaths}
}

// DeniedPathKey builds an LPM trie key. prefixlen is the path length in bits, as in prehook.c.
func DeniedPathKey(path string) (PreHookDeniedPathKey, error) {
	if path == "" {
		return PreHookDeniedPathKey{}, fmt.Errorf("denied path is empty")
	}
	if len(path) >= len(PreHookDeniedPathKey{}.Data) {
		return PreHookDeniedPathKey{}, fmt.Errorf("denied path too long (max %d bytes)", len(PreHookDeniedPathKey{}.Data)-1)
	}

	key := PreHookDeniedPathKey{
		Prefixlen: uint32(len(path)) * 8,
	}
	for i := 0; i < len(path); i++ {
		key.Data[i] = int8(path[i])
	}
	return key, nil
}

// DenyPath marks a path prefix as denied. A trailing slash matters for prefixes:
//   - "/etc/shadow"  denies that exact file (and paths with that prefix)
//   - "/home/"       denies everything under /home/
func (d *DeniedPathsController) DenyPath(path string) error {
	key, err := DeniedPathKey(path)
	if err != nil {
		return err
	}
	marker := DeniedPathMarker
	if err := d.deniedPaths.Update(&key, &marker, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("deny path %q: %w", path, err)
	}
	return nil
}

// AllowPath removes a previously denied path prefix.
func (d *DeniedPathsController) AllowPath(path string) error {
	key, err := DeniedPathKey(path)
	if err != nil {
		return err
	}
	if err := d.deniedPaths.Delete(&key); err != nil {
		return fmt.Errorf("allow path %q: %w", path, err)
	}
	return nil
}

// DenyPaths registers multiple denied prefixes.
func (d *DeniedPathsController) DenyPaths(paths []string) error {
	for _, path := range paths {
		if err := d.DenyPath(path); err != nil {
			return err
		}
	}
	return nil
}

// LoadDefaults applies DefaultDeniedPaths.
func (d *DeniedPathsController) LoadDefaults() error {
	return d.DenyPaths(DefaultDeniedPaths)
}
