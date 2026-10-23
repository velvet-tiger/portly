package registry

import "fmt"

// State is what is happening on one port.
type State string

const (
	// StateFree means no claim, no visible listener, and a test bind succeeded.
	StateFree State = "free"
	// StateListening means something is bound and nothing claims the port.
	StateListening State = "listening"
	// StateRegistered means the port is claimed and nothing is bound yet.
	StateRegistered State = "registered"
	// StateActive means the port is claimed and the claim's own project, or
	// its tied process, holds it.
	StateActive State = "active"
	// StateConflict means the port is claimed but something outside the
	// claim's project holds it, or a test bind failed with no visible holder.
	StateConflict State = "conflict"
	// StateUnavailable means nothing claims the port and portly sees no
	// listener, yet a test bind failed. Another user's process may hold it.
	StateUnavailable State = "unavailable"
)

// PortStatus is the state of one port, with the claim and holders behind it.
type PortStatus struct {
	Port  uint32
	State State
	// Claim is nil when the port is unclaimed.
	Claim   *Claim
	Holders []Holder
}

// Used reports whether anything claims or holds the port.
func (s PortStatus) Used() bool {
	return s.State != StateFree
}

// Statuses reports the state of every port in portRange.
//
// It runs a test bind on each port with no visible holder. A bind failure is
// the only evidence of a port held by a process portly cannot see.
func Statuses(current Registry, holders []Holder, checker BindChecker, portRange PortRange) ([]PortStatus, error) {
	statuses := make([]PortStatus, 0, portRange.Size())

	for port := portRange.First; port <= portRange.Last; port++ {
		status := PortStatus{Port: port, Holders: holdersOn(holders, port)}
		if claim, found := current.ByPort(port); found {
			claimCopy := claim
			status.Claim = &claimCopy
		}

		state, err := stateOf(status, checker)
		if err != nil {
			return nil, err
		}
		status.State = state
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// stateOf decides the state of one port from its claim and holders.
func stateOf(status PortStatus, checker BindChecker) (State, error) {
	claim := status.Claim

	if len(status.Holders) > 0 {
		if claim == nil {
			return StateListening, nil
		}
		for _, holder := range status.Holders {
			if !holder.belongsTo(claim.ProjectDir, claim.Project, claim.PID) {
				return StateConflict, nil
			}
		}
		return StateActive, nil
	}

	available, err := checker.Available(status.Port)
	if err != nil {
		return "", fmt.Errorf("checking whether port %d is free: %w", status.Port, err)
	}

	switch {
	case claim == nil && available:
		return StateFree, nil
	case claim == nil:
		return StateUnavailable, nil
	case available:
		return StateRegistered, nil
	default:
		return StateConflict, nil
	}
}
