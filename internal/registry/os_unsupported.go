//go:build !darwin && !linux

package registry

import (
	"fmt"
	"runtime"
	"time"
)

// lockFile always fails, naming the platform.
func lockFile(_ string, _ bool, _ time.Duration) (func(), error) {
	return nil, fmt.Errorf("portly cannot lock the port registry on %s", runtime.GOOS)
}

// OSProcessChecker stands in on platforms with no process check.
type OSProcessChecker struct{}

// Running reports every process as running. That keeps every PID-tied claim,
// which is the safe direction. lockFile fails on this platform first, so no
// registry command reaches this in practice.
func (OSProcessChecker) Running(int32) bool {
	return true
}

// OSBindChecker stands in on platforms with no test bind.
type OSBindChecker struct{}

// Available always fails, naming the platform.
func (OSBindChecker) Available(uint32) (bool, error) {
	return false, fmt.Errorf("portly cannot test-bind ports on %s", runtime.GOOS)
}
