//go:build darwin

package scan

import (
	"context"
	"fmt"

	gonet "github.com/shirou/gopsutil/v4/net"
)

// DarwinSocketTable reads listening TCP sockets through gopsutil, which on
// Darwin calls proc_pidinfo directly rather than shelling out to lsof.
type DarwinSocketTable struct{}

// NewSocketTable returns the socket reader for the current platform.
func NewSocketTable() SocketTable {
	return DarwinSocketTable{}
}

// Listening returns every TCP socket in the LISTEN state.
//
// Without elevated privileges the kernel only reports sockets owned by the
// calling user. Ports held by other users are absent from the result and are
// not reported as an error.
func (DarwinSocketTable) Listening(ctx context.Context) ([]Socket, error) {
	connections, err := gonet.ConnectionsWithContext(ctx, "tcp")
	if err != nil {
		return nil, fmt.Errorf("reading the TCP socket table: %w", err)
	}

	sockets := make([]Socket, 0, len(connections))
	for _, connection := range connections {
		if connection.Status != "LISTEN" {
			continue
		}
		// A socket with no PID cannot be attributed to a process, which makes it
		// useless for every enrichment step that follows.
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
