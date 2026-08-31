package ebpf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"

	"github.com/cilium/ebpf/ringbuf"
)

// AuditEvent mirrors struct audit_event in ebpf/prehook.c exactly -
// field order and sizes must match the C struct layout.
type AuditEvent struct {
	CgroupID uint64
	PID      uint32
	Action   uint8 // PermExec / PermFile / PermNet
	Allowed  uint8
	Comm     [16]byte
}

func (e AuditEvent) ActionName() string {
	switch e.Action {
	case PermExec:
		return "exec"
	case PermFile:
		return "file_open"
	case PermNet:
		return "socket_connect"
	default:
		return "unknown"
	}
}

func (e AuditEvent) CommString() string {
	return string(bytes.TrimRight(e.Comm[:], "\x00"))
}

// StartAuditReader reads events in a loop and sends them on the returned
// channel. Run this once, in its own goroutine, for the process lifetime.
// Call stop() on shutdown to unblock the reader and let the goroutine exit.
func StartAuditReader(c *LSMController) (events <-chan AuditEvent, stop func(), err error) {
	rd, err := ringbuf.NewReader(c.objs.AuditEvents)
	if err != nil {
		return nil, nil, fmt.Errorf("opening ring buffer reader: %w", err)
	}

	ch := make(chan AuditEvent, 256)

	go func() {
		defer close(ch)
		for {
			record, err := rd.Read()
			if err != nil {
				if err == ringbuf.ErrClosed {
					return // stop() was called
				}
				log.Printf("audit ring buffer read error: %v", err)
				continue
			}

			var ev AuditEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &ev); err != nil {
				log.Printf("decoding audit event: %v", err)
				continue
			}

			select {
			case ch <- ev:
			default:
				// consumer too slow; drop rather than block the kernel-side producer
				log.Printf("audit event dropped (channel full): cgroup=%d action=%s", ev.CgroupID, ev.ActionName())
			}
		}
	}()

	return ch, func() { rd.Close() }, nil
}