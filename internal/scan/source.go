package scan

import "context"

// SocketTable reads listening sockets from the operating system.
//
// Implementations are platform specific. Darwin and Linux share the gopsutil
// reader in socket_gopsutil.go; other platforms get the failing fallback in
// socket_unsupported.go.
type SocketTable interface {
	// Listening returns every socket in the LISTEN state. Sockets the caller
	// lacks permission to see are omitted by the OS rather than reported as an
	// error, so a short result is not distinguishable from a quiet machine.
	Listening(ctx context.Context) ([]Socket, error)
}

// ProcessTable describes running processes by PID.
type ProcessTable interface {
	// Describe returns details for one PID. It returns an error when the process
	// has exited between the socket read and this call, which is expected and
	// which callers should treat as "skip this row", not as a fatal condition.
	Describe(ctx context.Context, pid int32) (Process, error)
}

// ContainerIndex maps published host ports to the containers behind them.
type ContainerIndex interface {
	// ByHostPort returns containers keyed by the host port they publish. A nil
	// map with a nil error means the container runtime is not running, which is
	// an ordinary state and not a failure.
	ByHostPort(ctx context.Context) (map[uint32]Container, error)
}
