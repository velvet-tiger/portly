//go:build darwin || linux

package registry

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests touch real sockets, locks and processes. They are kept apart
// from the pure tests so a failure here points at the operating system layer.

func TestOSBindCheckerSeesAWildcardListener(t *testing.T) {
	assertListenerBlocksBind(t, "tcp4", "0.0.0.0:0")
}

func TestOSBindCheckerSeesALoopbackOnlyListener(t *testing.T) {
	assertListenerBlocksBind(t, "tcp4", "127.0.0.1:0")
}

func TestOSBindCheckerReportsAClosedPortAvailable(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("opening a listener: %v", err)
	}
	port := uint32(listener.Addr().(*net.TCPAddr).Port)
	if err := listener.Close(); err != nil {
		t.Fatalf("closing the listener: %v", err)
	}

	available, err := OSBindChecker{}.Available(port)
	if err != nil {
		t.Fatalf("Available(%d) returned %v", port, err)
	}
	if !available {
		t.Errorf("Available(%d) = false after the listener closed, want true", port)
	}
}

func assertListenerBlocksBind(t *testing.T, network, address string) {
	t.Helper()

	listener, err := net.Listen(network, address)
	if err != nil {
		t.Fatalf("opening a listener on %s: %v", address, err)
	}
	defer listener.Close()
	port := uint32(listener.Addr().(*net.TCPAddr).Port)

	available, err := OSBindChecker{}.Available(port)
	if err != nil {
		t.Fatalf("Available(%d) returned %v", port, err)
	}
	if available {
		t.Errorf("Available(%d) = true while %s listens on it, want false", port, address)
	}
}

func TestOSProcessCheckerSeesThisProcess(t *testing.T) {
	if !(OSProcessChecker{}).Running(int32(os.Getpid())) {
		t.Error("Running(own pid) = false, want true")
	}
}

func TestOSProcessCheckerReportsAnUnusedPIDAsExited(t *testing.T) {
	// Both kernels cap PIDs far below this. Linux allows at most 4194304.
	if (OSProcessChecker{}).Running(2_000_000_000) {
		t.Error("Running(2000000000) = true, want false")
	}
}

func TestLockFileTimesOutWhileAnotherHolderHasIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ports.json.lock")

	unlock, err := lockFile(path, true, time.Second)
	if err != nil {
		t.Fatalf("first lockFile() returned %v", err)
	}
	defer unlock()

	_, err = lockFile(path, true, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "held the port registry lock") {
		t.Fatalf("second lockFile() returned %v, want a timeout naming the lock", err)
	}
}

func TestLockFileAllowsSharedReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ports.json.lock")

	first, err := lockFile(path, false, time.Second)
	if err != nil {
		t.Fatalf("first shared lockFile() returned %v", err)
	}
	defer first()

	second, err := lockFile(path, false, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("second shared lockFile() returned %v, want shared locks to coexist", err)
	}
	second()
}
