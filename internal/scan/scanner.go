package scan

import (
	"context"
	"sort"
)

// Scanner turns the operating system's socket table into described listeners.
//
// Its collaborators are injected so the whole pipeline can be exercised without
// touching the running system.
type Scanner struct {
	sockets    SocketTable
	processes  ProcessTable
	containers ContainerIndex
}

// NewScanner builds a Scanner from its three sources. containers may be nil,
// which disables container attribution.
func NewScanner(sockets SocketTable, processes ProcessTable, containers ContainerIndex) *Scanner {
	return &Scanner{sockets: sockets, processes: processes, containers: containers}
}

// Scan reads every listening socket and describes the process behind it.
//
// Processes that exit mid-scan are skipped rather than failing the run, because
// a dev server shutting down while portly reads is ordinary. A failure to reach
// the container runtime is likewise non-fatal: it costs container names, not
// the whole result.
func (s *Scanner) Scan(ctx context.Context) ([]Listener, error) {
	sockets, err := s.sockets.Listening(ctx)
	if err != nil {
		return nil, err
	}

	byPort := s.containersByPort(ctx)

	described := make(map[processPort]*Listener, len(sockets))
	var order []processPort

	for _, socket := range sockets {
		key := processPort{PID: socket.PID, Port: socket.Port, Protocol: socket.Protocol}

		// One process listening on both IPv4 and IPv6 produces two socket rows.
		// Merge them into one listener carrying both addresses.
		if existing, seen := described[key]; seen {
			existing.Addresses = appendAddress(existing.Addresses, socket.Address)
			continue
		}

		process, err := s.processes.Describe(ctx, socket.PID)
		if err != nil {
			continue
		}

		listener := &Listener{
			Protocol:  socket.Protocol,
			Port:      socket.Port,
			Addresses: appendAddress(nil, socket.Address),
			Process:   process,
		}
		if container, published := byPort[socket.Port]; published {
			copied := container
			listener.Container = &copied
		}

		described[key] = listener
		order = append(order, key)
	}

	listeners := make([]Listener, 0, len(order))
	for _, key := range order {
		listeners = append(listeners, *described[key])
	}

	return sortListeners(collapseWorkerPools(listeners)), nil
}

// containersByPort looks up published container ports, tolerating an absent or
// unreachable container runtime.
func (s *Scanner) containersByPort(ctx context.Context) map[uint32]Container {
	if s.containers == nil {
		return nil
	}
	byPort, err := s.containers.ByHostPort(ctx)
	if err != nil {
		// Docker not running is the common case and is not worth failing over.
		// The cost is container names on container rows, which the caller can
		// see for itself in the output.
		return nil
	}
	return byPort
}

// processPort identifies one process's hold on one port.
type processPort struct {
	PID      int32
	Port     uint32
	Protocol string
}

// appendAddress adds an address unless it is already present.
func appendAddress(addresses []string, address string) []string {
	if address == "" {
		return addresses
	}
	for _, existing := range addresses {
		if existing == address {
			return addresses
		}
	}
	return append(addresses, address)
}

// collapseWorkerPools merges processes that share a port under one executable
// name into a single listener.
//
// A php-fpm pool binds one socket and serves it from a master plus its workers,
// which the socket table reports as several processes on the same port. Listing
// each separately triples the row count and says nothing useful. The lowest PID
// is kept, which is the master in every pool model that forks its workers.
//
// Two unrelated servers sharing a port through SO_REUSEPORT would also collapse
// if they ran the same executable. That is rare, and the merged row still
// carries every PID in SiblingPIDs, so nothing is lost beyond a separate line.
func collapseWorkerPools(listeners []Listener) []Listener {
	type portName struct {
		Protocol string
		Port     uint32
		Name     string
	}

	primary := make(map[portName]int, len(listeners))
	result := make([]Listener, 0, len(listeners))

	for _, listener := range listeners {
		key := portName{
			Protocol: listener.Protocol,
			Port:     listener.Port,
			Name:     listener.Process.Name,
		}

		index, seen := primary[key]
		if !seen {
			primary[key] = len(result)
			result = append(result, listener)
			continue
		}

		kept := &result[index]
		if listener.Process.PID < kept.Process.PID {
			// The new row is the master. Demote the previously kept row.
			listener.SiblingPIDs = append(listener.SiblingPIDs, kept.SiblingPIDs...)
			listener.SiblingPIDs = append(listener.SiblingPIDs, kept.Process.PID)
			for _, address := range kept.Addresses {
				listener.Addresses = appendAddress(listener.Addresses, address)
			}
			result[index] = listener
			continue
		}

		kept.SiblingPIDs = append(kept.SiblingPIDs, listener.Process.PID)
		for _, address := range listener.Addresses {
			kept.Addresses = appendAddress(kept.Addresses, address)
		}
	}

	for i := range result {
		sort.Slice(result[i].SiblingPIDs, func(a, b int) bool {
			return result[i].SiblingPIDs[a] < result[i].SiblingPIDs[b]
		})
	}
	return result
}

// sortListeners orders results by port, then by PID, so repeated runs against
// an unchanged machine produce identical output.
func sortListeners(listeners []Listener) []Listener {
	sort.Slice(listeners, func(a, b int) bool {
		if listeners[a].Port != listeners[b].Port {
			return listeners[a].Port < listeners[b].Port
		}
		return listeners[a].Process.PID < listeners[b].Process.PID
	})
	return listeners
}
