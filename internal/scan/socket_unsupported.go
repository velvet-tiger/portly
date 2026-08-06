//go:build !darwin

package scan

import (
	"context"
	"fmt"
	"runtime"
)

// UnsupportedSocketTable stands in on platforms with no socket reader.
//
// portly's first release targets Darwin only. gopsutil can read the socket
// table on Linux via /proc/net/tcp and on Windows via GetExtendedTcpTable, so
// adding those readers is small. Neither has been run, so neither is claimed to
// work. Windows additionally needs a reader for WSL2, where dev servers surface
// on the host behind a relay process with no attributable PID.
type UnsupportedSocketTable struct{}

// NewSocketTable returns the socket reader for the current platform.
func NewSocketTable() SocketTable {
	return UnsupportedSocketTable{}
}

// Listening always fails, naming the platform and what would be required.
func (UnsupportedSocketTable) Listening(_ context.Context) ([]Socket, error) {
	return nil, fmt.Errorf(
		"portly has no socket reader for %s: only darwin is implemented",
		runtime.GOOS,
	)
}
