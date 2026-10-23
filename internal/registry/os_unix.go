//go:build darwin || linux

package registry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"
	"time"
)

// lockRetryInterval is how often a blocked lock is retried.
const lockRetryInterval = 20 * time.Millisecond

// lockFile takes a flock on path, creating the file if needed. It returns a
// function that releases the lock.
//
// The lock is taken non-blocking and retried until timeout, so a wedged
// holder produces an error naming the lock file instead of a hang.
func lockFile(path string, exclusive bool, timeout time.Duration) (func(), error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the port registry lock: %w", err)
	}

	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}

	deadline := time.Now().Add(timeout)
	for {
		err = syscall.Flock(int(file.Fd()), mode|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = file.Close()
			return nil, fmt.Errorf("locking the port registry: %w", err)
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, fmt.Errorf("another portly held the port registry lock %s for over %s", path, timeout)
		}
		time.Sleep(lockRetryInterval)
	}

	return func() {
		// Closing the descriptor releases the flock even if the explicit
		// unlock fails, so neither error leaves the registry locked.
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

// OSProcessChecker asks the operating system whether a process exists.
type OSProcessChecker struct{}

// Running sends signal 0, which checks for the process without affecting it.
//
// Only ESRCH means the process is gone. EPERM means it exists but belongs to
// another user. kill(2) documents no other failure for signal 0, so any other
// error also counts as running: dropping a live claim by mistake is worse than
// keeping a dead one.
func (OSProcessChecker) Running(pid int32) bool {
	err := syscall.Kill(int(pid), 0)
	return !errors.Is(err, syscall.ESRCH)
}

// bindTargets are the addresses a test bind tries. The wildcard binds catch a
// listener on any address. The loopback binds catch one bound to loopback
// alone, which macOS lets coexist with a wildcard bind.
var bindTargets = []struct {
	network string
	host    string
}{
	{"tcp4", "0.0.0.0"},
	{"tcp6", "::"},
	{"tcp4", "127.0.0.1"},
	{"tcp6", "::1"},
}

// OSBindChecker test-binds ports through the operating system.
type OSBindChecker struct{}

// Available reports whether port can be bound on every target address.
//
// Address reuse is turned off for the test socket. With SO_REUSEADDR, which
// Go sets by default, macOS lets a wildcard bind succeed beside an existing
// loopback listener, and the check would miss it. The cost is that a port with
// connections still in TIME_WAIT reads as unavailable for a minute or so.
//
// A target whose address family the host lacks, such as IPv6 on a machine
// with it disabled, is skipped rather than counted as in use.
func (OSBindChecker) Available(port uint32) (bool, error) {
	config := net.ListenConfig{Control: disableAddressReuse}

	for _, target := range bindTargets {
		address := net.JoinHostPort(target.host, strconv.FormatUint(uint64(port), 10))
		listener, err := config.Listen(context.Background(), target.network, address)
		if err == nil {
			if closeErr := listener.Close(); closeErr != nil {
				return false, fmt.Errorf("closing the test bind on %s: %w", address, closeErr)
			}
			continue
		}

		switch {
		case errors.Is(err, syscall.EADDRINUSE), errors.Is(err, syscall.EACCES):
			return false, nil
		case errors.Is(err, syscall.EADDRNOTAVAIL), errors.Is(err, syscall.EAFNOSUPPORT), errors.Is(err, syscall.EPROTONOSUPPORT):
			continue
		default:
			return false, fmt.Errorf("test-binding %s: %w", address, err)
		}
	}
	return true, nil
}

// disableAddressReuse clears SO_REUSEADDR on a socket before it binds.
func disableAddressReuse(_, _ string, connection syscall.RawConn) error {
	var optionErr error
	err := connection.Control(func(descriptor uintptr) {
		optionErr = syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 0)
	})
	if err != nil {
		return err
	}
	return optionErr
}
