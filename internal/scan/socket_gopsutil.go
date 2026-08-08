//go:build darwin || linux

package scan

import (
	"context"
	"fmt"

	gonet "github.com/shirou/gopsutil/v4/net"
)

// GopsutilSocketTable reads listening TCP sockets through gopsutil. On Darwin
// that calls proc_pidinfo directly rather than shelling out to lsof. On Linux
// it reads /proc/net/tcp and /proc/net/tcp6, then attributes each socket to a
// process by walking /proc/<pid>/fd.
type GopsutilSocketTable struct{}

// NewSocketTable returns the socket reader for the current platform.
func NewSocketTable() SocketTable {
	return GopsutilSocketTable{}
}

// Listening returns every TCP socket in the LISTEN state.
//
// Without elevated privileges, a socket owned by another user is absent from
// the result and is not reported as an error. On Darwin the kernel omits it;
// on Linux the socket is listed but its owning process cannot be read, and a
// socket with no PID is dropped because it cannot be attributed, which makes
// it useless for every enrichment step that follows.
func (GopsutilSocketTable) Listening(ctx context.Context) ([]Socket, error) {
	connections, err := gonet.ConnectionsWithContext(ctx, "tcp")
	if err != nil {
		return nil, fmt.Errorf("reading the TCP socket table: %w", err)
	}

	sockets := make([]Socket, 0, len(connections))
	for _, connection := range connections {
		if connection.Status != "LISTEN" {
			continue
		}
		if connection.Pid == 0 {
			continue
		}
		sockets = append(sockets, Socket{
			Protocol: "tcp",
			Port:     connection.Laddr.Port,
			Address:  connection.Laddr.IP,
			PID:      connection.Pid,
		})
	}
	return sockets, nil
}
