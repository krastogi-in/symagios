//go:build !linux

package cgroup

import (
	"context"
	"fmt"
)

// Group is a no-op stub on non-Linux platforms.
type Group struct{}

// Create is unsupported outside Linux.
func Create(ctx context.Context, name string) (*Group, error) {
	return nil, fmt.Errorf("cgroup: unsupported on this platform")
}

// ID returns 0 on non-Linux platforms.
func (g *Group) ID() uint64 { return 0 }

// Teardown is a no-op on non-Linux platforms.
func (g *Group) Teardown(ctx context.Context) error { return nil }
