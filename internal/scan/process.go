package scan

import (
	"context"
	"fmt"
	"time"

	goprocess "github.com/shirou/gopsutil/v4/process"
)

// maxAncestryDepth bounds the walk up the parent chain. Process trees are
// shallow, and a bound removes any possibility of looping on a malformed or
// recycled PID.
const maxAncestryDepth = 12

// SystemProcessTable reads process details through gopsutil.
type SystemProcessTable struct{}

// NewProcessTable returns a ProcessTable backed by the running system.
func NewProcessTable() ProcessTable {
	return SystemProcessTable{}
}

// Describe returns details for pid, including its chain of parents.
//
// Fields the OS declines to report are left at their zero value rather than
// failing the whole call, because a missing command line is no reason to drop
// an otherwise usable row. The working directory is the exception: it is
// modelled so callers can tell "unknown" from "empty".
func (t SystemProcessTable) Describe(ctx context.Context, pid int32) (Process, error) {
	handle, err := goprocess.NewProcessWithContext(ctx, pid)
	if err != nil {
		return Process{}, fmt.Errorf("opening process %d: %w", pid, err)
	}

	described := Process{PID: pid}
	described.Name, _ = handle.NameWithContext(ctx)
	described.Executable, _ = handle.ExeWithContext(ctx)
	described.CommandLine, _ = handle.CmdlineWithContext(ctx)
	described.Arguments, _ = handle.CmdlineSliceWithContext(ctx)
	described.User, _ = handle.UsernameWithContext(ctx)

	if parent, err := handle.PpidWithContext(ctx); err == nil {
		described.ParentPID = parent
	}
	if started, err := handle.CreateTimeWithContext(ctx); err == nil && started > 0 {
		described.StartedAt = time.UnixMilli(started)
	}
	described.WorkingDir = readWorkingDirectory(ctx, handle)
	described.Ancestry = walkAncestry(ctx, described.ParentPID)

	return described, nil
}

// readWorkingDirectory reads a process's current directory, treating an empty
// path as unknown.
//
// gopsutil's Windows implementation returns an empty string and a nil error
// when the OS denies access. Collapsing both cases to "unknown" keeps callers
// from presenting a denied read as a real answer.
func readWorkingDirectory(ctx context.Context, handle *goprocess.Process) WorkingDirectory {
	path, err := handle.CwdWithContext(ctx)
	if err != nil || path == "" {
		return UnknownDirectory()
	}
	return KnownDirectory(path)
}

// walkAncestry follows parent PIDs outwards from startPID.
//
// The walk stops at PID 1, at the depth bound, or at the first process the OS
// will not describe. A truncated chain is returned as-is; the caller cannot
// distinguish it from a complete one, and does not need to, because every
// consumer asks "is any known agent in here" rather than "is this exhaustive".
func walkAncestry(ctx context.Context, startPID int32) []Ancestor {
	var chain []Ancestor
	current := startPID

	for depth := 0; depth < maxAncestryDepth; depth++ {
		if current <= 1 {
			return chain
		}
		if err := ctx.Err(); err != nil {
			return chain
		}

		handle, err := goprocess.NewProcessWithContext(ctx, current)
		if err != nil {
			return chain
		}

		ancestor := Ancestor{PID: current}
		ancestor.Name, _ = handle.NameWithContext(ctx)
		ancestor.Executable, _ = handle.ExeWithContext(ctx)
		chain = append(chain, ancestor)

		parent, err := handle.PpidWithContext(ctx)
		if err != nil || parent == current {
			return chain
		}
		current = parent
	}
	return chain
}
