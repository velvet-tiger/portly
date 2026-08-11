// Package kill signals the process behind a listening port.
package kill

import (
	"fmt"
	"os"
	"syscall"
)

// Signaller sends an OS signal to a process by PID.
//
// It is an interface so the command that chooses which PIDs to signal can be
// tested without touching real processes.
type Signaller interface {
	Signal(pid int32, signal syscall.Signal) error
}

// OSSignaller signals processes through the operating system.
type OSSignaller struct{}

// Signal finds the process by pid and delivers signal to it.
//
// os.FindProcess never fails on Unix even for a PID that no longer exists; the
// failure surfaces from the Signal call instead, which this wraps with the PID
// so the caller can report exactly which target was unreachable.
func (OSSignaller) Signal(pid int32, signal syscall.Signal) error {
	process, err := os.FindProcess(int(pid))
	if err != nil {
		return fmt.Errorf("finding process %d: %w", pid, err)
	}
	if err := process.Signal(signal); err != nil {
		return fmt.Errorf("signalling process %d: %w", pid, err)
	}
	return nil
}
