package scan

import (
	"context"
	"errors"
	"testing"
)

// fixedSocketTable returns a prepared socket list.
type fixedSocketTable struct {
	sockets []Socket
	err     error
}

func (f fixedSocketTable) Listening(context.Context) ([]Socket, error) {
	return f.sockets, f.err
}

// fixedProcessTable answers from a prepared map, reporting an error for any PID
// it does not hold, which models a process exiting mid-scan.
type fixedProcessTable struct {
	processes map[int32]Process
}

func (f fixedProcessTable) Describe(_ context.Context, pid int32) (Process, error) {
	process, found := f.processes[pid]
	if !found {
		return Process{}, errors.New("no such process")
	}
	return process, nil
}

// fixedContainerIndex returns a prepared port mapping.
type fixedContainerIndex struct {
	byPort map[uint32]Container
	err    error
}

func (f fixedContainerIndex) ByHostPort(context.Context) (map[uint32]Container, error) {
	return f.byPort, f.err
}

func TestScanMergesAddressFamiliesOntoOneListener(t *testing.T) {
	scanner := NewScanner(
		fixedSocketTable{sockets: []Socket{
			{Protocol: "tcp", Port: 7000, Address: "127.0.0.1", PID: 10},
			{Protocol: "tcp", Port: 7000, Address: "::1", PID: 10},
		}},
		fixedProcessTable{processes: map[int32]Process{10: {PID: 10, Name: "node"}}},
		nil,
	)

	listeners, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() returned %v", err)
	}
	if len(listeners) != 1 {
		t.Fatalf("got %d listeners, want 1; IPv4 and IPv6 rows for one process are one listener", len(listeners))
	}
	if len(listeners[0].Addresses) != 2 {
		t.Errorf("Addresses = %v, want both families recorded", listeners[0].Addresses)
	}
}

func TestScanCollapsesAWorkerPoolOntoItsMaster(t *testing.T) {
	scanner := NewScanner(
		fixedSocketTable{sockets: []Socket{
			{Protocol: "tcp", Port: 9000, Address: "127.0.0.1", PID: 3561},
			{Protocol: "tcp", Port: 9000, Address: "127.0.0.1", PID: 3421},
			{Protocol: "tcp", Port: 9000, Address: "127.0.0.1", PID: 3563},
		}},
		fixedProcessTable{processes: map[int32]Process{
			3421: {PID: 3421, Name: "php-fpm"},
			3561: {PID: 3561, Name: "php-fpm"},
			3563: {PID: 3563, Name: "php-fpm"},
		}},
		nil,
	)

	listeners, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() returned %v", err)
	}
	if len(listeners) != 1 {
		t.Fatalf("got %d listeners, want 1 collapsed pool", len(listeners))
	}
	if listeners[0].Process.PID != 3421 {
		t.Errorf("kept PID %d, want the lowest PID 3421 as the master", listeners[0].Process.PID)
	}
	if len(listeners[0].SiblingPIDs) != 2 {
		t.Errorf("SiblingPIDs = %v, want the two workers", listeners[0].SiblingPIDs)
	}
}

func TestScanKeepsDistinctProcessesOnTheSamePortApart(t *testing.T) {
	scanner := NewScanner(
		fixedSocketTable{sockets: []Socket{
			{Protocol: "tcp", Port: 8080, Address: "127.0.0.1", PID: 10},
			{Protocol: "tcp", Port: 8080, Address: "127.0.0.1", PID: 20},
		}},
		fixedProcessTable{processes: map[int32]Process{
			10: {PID: 10, Name: "node"},
			20: {PID: 20, Name: "python"},
		}},
		nil,
	)

	listeners, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() returned %v", err)
	}
	if len(listeners) != 2 {
		t.Fatalf("got %d listeners, want 2; different executables are not one pool", len(listeners))
	}
}

func TestScanSkipsProcessesThatExitMidScan(t *testing.T) {
	scanner := NewScanner(
		fixedSocketTable{sockets: []Socket{
			{Protocol: "tcp", Port: 3000, Address: "127.0.0.1", PID: 10},
			{Protocol: "tcp", Port: 3001, Address: "127.0.0.1", PID: 999},
		}},
		fixedProcessTable{processes: map[int32]Process{10: {PID: 10, Name: "node"}}},
		nil,
	)

	listeners, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("a process exiting mid-scan must not fail the run, got %v", err)
	}
	if len(listeners) != 1 {
		t.Fatalf("got %d listeners, want the one still-running process", len(listeners))
	}
}

func TestScanSurvivesAnUnreachableContainerRuntime(t *testing.T) {
	scanner := NewScanner(
		fixedSocketTable{sockets: []Socket{{Protocol: "tcp", Port: 3000, Address: "127.0.0.1", PID: 10}}},
		fixedProcessTable{processes: map[int32]Process{10: {PID: 10, Name: "node"}}},
		fixedContainerIndex{err: errors.New("docker daemon is not running")},
	)

	listeners, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("docker being unavailable must not fail the scan, got %v", err)
	}
	if len(listeners) != 1 || listeners[0].Container != nil {
		t.Error("want the listener reported with no container attribution")
	}
}

func TestScanAttachesContainersByHostPort(t *testing.T) {
	scanner := NewScanner(
		fixedSocketTable{sockets: []Socket{{Protocol: "tcp", Port: 8000, Address: "*", PID: 10}}},
		fixedProcessTable{processes: map[int32]Process{10: {PID: 10, Name: "com.docker.backend"}}},
		fixedContainerIndex{byPort: map[uint32]Container{8000: {Name: "api-1", ComposeProject: "shop"}}},
	)

	listeners, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() returned %v", err)
	}
	if listeners[0].Container == nil || listeners[0].Container.Name != "api-1" {
		t.Error("want the published port attributed to container api-1")
	}
}

func TestScanFailsWhenTheSocketTableFails(t *testing.T) {
	scanner := NewScanner(
		fixedSocketTable{err: errors.New("no reader for this platform")},
		fixedProcessTable{},
		nil,
	)

	if _, err := scanner.Scan(context.Background()); err == nil {
		t.Error("an unreadable socket table must fail the scan rather than report zero ports")
	}
}

func TestScanOrdersResultsByPort(t *testing.T) {
	scanner := NewScanner(
		fixedSocketTable{sockets: []Socket{
			{Protocol: "tcp", Port: 9000, Address: "127.0.0.1", PID: 30},
			{Protocol: "tcp", Port: 3000, Address: "127.0.0.1", PID: 10},
			{Protocol: "tcp", Port: 5000, Address: "127.0.0.1", PID: 20},
		}},
		fixedProcessTable{processes: map[int32]Process{
			10: {PID: 10, Name: "a"}, 20: {PID: 20, Name: "b"}, 30: {PID: 30, Name: "c"},
		}},
		nil,
	)

	listeners, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() returned %v", err)
	}
	for i, want := range []uint32{3000, 5000, 9000} {
		if listeners[i].Port != want {
			t.Errorf("listener %d is port %d, want %d", i, listeners[i].Port, want)
		}
	}
}
