// Package scan reads the operating system's socket table and describes the
// processes behind each listening port.
package scan

import "time"

// WorkingDirectory is a process's current directory.
//
// Known distinguishes "the OS told us the directory" from "the OS refused to
// say". The distinction is not cosmetic: on Windows gopsutil reports an
// access-denied CWD as an empty string with a nil error, so an absent value is
// indistinguishable from a successful empty read unless it is modelled
// explicitly.
type WorkingDirectory struct {
	Path  string
	Known bool
}

// KnownDirectory returns a WorkingDirectory the OS reported successfully.
func KnownDirectory(path string) WorkingDirectory {
	return WorkingDirectory{Path: path, Known: true}
}

// UnknownDirectory returns a WorkingDirectory the OS declined to report.
func UnknownDirectory() WorkingDirectory {
	return WorkingDirectory{}
}

// Ancestor is one entry in a process's chain of parents.
type Ancestor struct {
	PID  int32
	Name string
	// Executable is the full path, which distinguishes an editor's own helper
	// processes from a shell the editor happened to spawn.
	Executable string
}

// Process describes the process holding a listening socket.
type Process struct {
	PID         int32
	ParentPID   int32
	Name        string
	Executable  string
	CommandLine string
	// Arguments is argv. Classification matches against these tokens rather than
	// the joined command line, so a framework name occurring inside an unrelated
	// path cannot be mistaken for the framework itself.
	Arguments  []string
	User       string
	WorkingDir WorkingDirectory
	StartedAt  time.Time
	// Ancestry runs from the immediate parent outwards. It stops at PID 1 or at
	// the first process the OS declines to describe.
	Ancestry []Ancestor
}

// Container describes a Docker container publishing a port to the host.
type Container struct {
	ID   string
	Name string
	// ComposeProject is the docker compose project name, derived from the
	// container name prefix. Empty when the container was not started by compose.
	ComposeProject string
	Image          string
	Status         string
}

// Listener is one port held open by one process.
//
// Addresses holds every bind address that resolved to this port and PID. A
// process listening on both IPv4 and IPv6 produces two socket table rows and
// one Listener.
type Listener struct {
	Protocol  string
	Port      uint32
	Addresses []string
	Process   Process
	// Container is set when the port is published by a Docker container. The
	// process behind such a port is Docker's own backend, which carries no
	// useful project information on its own.
	Container *Container
	// SiblingPIDs holds other processes listening on the same port under the
	// same executable name, such as a php-fpm worker pool.
	SiblingPIDs []int32
}

// Socket is a single row of the operating system's socket table, before any
// process details are attached.
type Socket struct {
	Protocol string
	Port     uint32
	Address  string
	PID      int32
}
