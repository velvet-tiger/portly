//go:build !darwin && !linux

package scan

import (
	"context"
	"fmt"
	"runtime"
)

// UnsupportedSocketTable stands in on platforms with no socket reader.
type UnsupportedSocketTable struct{}

// NewSocketTable returns the socket reader for the current platform.
func NewSocketTable() SocketTable {
	return UnsupportedSocketTable{}
}

// Listening always fails, naming the platform.
func (UnsupportedSocketTable) Listening(_ context.Context) ([]Socket, error) {
	return nil, fmt.Errorf(
		"portly has no socket reader for %s: only darwin and linux are implemented",
		runtime.GOOS,
	)
}
