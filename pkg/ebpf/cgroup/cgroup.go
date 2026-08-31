//go:build linux

package cgroup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const cgroupRoot = "/sys/fs/cgroup"

// Group is a cgroup v2 leaf cgroup with the worker thread attached.
type Group struct {
	path   string
	parent string
	id     uint64
}

// Create makes a leaf cgroup under the process's current cgroup and moves the
// current OS thread into it. Call runtime.LockOSThread() before Create.
func Create(ctx context.Context, name string) (*Group, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	parent, err := currentCgroupDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(parent, name)
	if err := os.Mkdir(path, 0o755); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("mkdir cgroup %s: %w", path, err)
	}

	tid := unix.Gettid()
	if err := writeProc(path, tid); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("attach thread %d to cgroup %s: %w", tid, path, err)
	}

	id, err := readCgroupID(path)
	if err != nil {
		_ = writeProc(parent, tid)
		_ = os.Remove(path)
		return nil, err
	}

	return &Group{path: path, parent: parent, id: id}, nil
}

// ID returns the cgroup v2 ID (matches bpf_get_current_cgroup_id()).
func (g *Group) ID() uint64 {
	if g == nil {
		return 0
	}
	return g.id
}

// Teardown moves the worker thread back to the parent cgroup and removes the leaf.
func (g *Group) Teardown(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	tid := unix.Gettid()
	parent := g.parent
	if parent == "" {
		parent = cgroupRoot
	}
	if err := writeProc(parent, tid); err != nil {
		return fmt.Errorf("move thread %d to parent cgroup %s: %w", tid, parent, err)
	}
	if err := os.Remove(g.path); err != nil {
		return fmt.Errorf("remove cgroup %s: %w", g.path, err)
	}
	return nil
}

func writeProc(cgroupPath string, tid int) error {
	procs := filepath.Join(cgroupPath, "cgroup.procs")
	return os.WriteFile(procs, []byte(fmt.Sprintf("%d\n", tid)), 0)
}

func readCgroupID(cgroupPath string) (uint64, error) {
	// Newer kernels expose cgroup.id; older ones (and some container mounts) do not.
	// bpf_get_current_cgroup_id() returns the cgroup directory inode either way.
	if data, err := os.ReadFile(filepath.Join(cgroupPath, "cgroup.id")); err == nil {
		id, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse cgroup.id: %w", err)
		}
		return id, nil
	}

	var st unix.Stat_t
	if err := unix.Stat(cgroupPath, &st); err != nil {
		return 0, fmt.Errorf("stat cgroup %s: %w", cgroupPath, err)
	}
	return st.Ino, nil
}

// currentCgroupDir returns the absolute path of this process's cgroup v2 dir.
func currentCgroupDir() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", fmt.Errorf("read /proc/self/cgroup: %w", err)
	}
	rel := ""
	for _, line := range strings.Split(string(data), "\n") {
		// cgroup v2: "0::/path"
		if strings.HasPrefix(line, "0::") {
			rel = strings.TrimPrefix(line, "0::")
			break
		}
	}
	if rel == "" {
		return "", fmt.Errorf("no cgroup v2 entry in /proc/self/cgroup")
	}
	if rel == "/" {
		return cgroupRoot, nil
	}
	return filepath.Join(cgroupRoot, rel), nil
}
