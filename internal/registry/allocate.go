package registry

import (
	"fmt"
	"path/filepath"
	"time"
)

// Holder is a process holding a port open, reduced to what claim decisions
// need.
type Holder struct {
	Port    uint32
	PID     int32
	Process string
	// ProjectDir is the root of the project the process runs in. It is empty
	// when no project was found.
	ProjectDir string
	// ProjectName stands in when no directory is known. A container started by
	// docker compose has a compose project name but no host directory.
	ProjectName string
}

// belongsTo reports whether the holder is part of the given claim's project,
// or is the process the claim is tied to.
//
// A holder whose project is unknown never belongs. Treating it as a match would
// hide a real conflict behind missing information.
func (h Holder) belongsTo(projectDir, projectName string, pid int32) bool {
	if pid != 0 && h.PID == pid {
		return true
	}
	if h.ProjectDir != "" {
		return h.ProjectDir == projectDir
	}
	if h.ProjectName != "" {
		return h.ProjectName == projectName
	}
	return false
}

// BindChecker reports whether a port can be bound right now.
//
// It catches ports held by processes portly cannot see in the socket table,
// such as another user's server.
type BindChecker interface {
	Available(port uint32) (bool, error)
}

// Request asks for a port for one app in one project.
type Request struct {
	ProjectDir string
	Project    string
	Name       string
	// Port is the exact port wanted. Zero means any free port in Range.
	Port  uint32
	Range PortRange
	// PID ties the claim to a process. Zero leaves it untied.
	PID int32
	Now time.Time
}

// validate rejects a request that cannot produce a meaningful claim.
func (r Request) validate() error {
	if r.Name == "" {
		return fmt.Errorf("a claim needs a name")
	}
	if !filepath.IsAbs(r.ProjectDir) {
		return fmt.Errorf("project directory %q is not absolute", r.ProjectDir)
	}
	if r.Project == "" {
		return fmt.Errorf("a claim needs a project name")
	}
	if r.Port == 0 && (r.Range.First == 0 || r.Range.First > r.Range.Last) {
		return fmt.Errorf("port range %s is not valid", r.Range)
	}
	return nil
}

// Allocate decides which port a request gets. It does not change the registry.
// The caller adds the returned claim with Registry.With.
//
// A project and name that already hold a claim get the same claim back, which
// makes registering idempotent and keeps each app on a stable port. Asking for
// a different exact port while holding one is an error rather than a move, so
// a typo cannot silently abandon a port other tools rely on.
func Allocate(current Registry, holders []Holder, checker BindChecker, request Request) (Claim, error) {
	if err := request.validate(); err != nil {
		return Claim{}, err
	}

	if existing, found := current.ByIdentity(request.ProjectDir, request.Name); found {
		if request.Port != 0 && request.Port != existing.Port {
			return Claim{}, fmt.Errorf("%s already holds port %d. Release it before claiming port %d",
				existing.Describe(), existing.Port, request.Port)
		}
		if request.PID != 0 {
			existing.PID = request.PID
		}
		return existing, nil
	}

	if request.Port != 0 {
		return claimExact(current, holders, checker, request)
	}
	return claimFirstFree(current, holders, checker, request)
}

// claimExact claims request.Port if nothing else holds it.
//
// A port already held by the request's own project is allowed. That lets an
// app register the port its server is already running on.
func claimExact(current Registry, holders []Holder, checker BindChecker, request Request) (Claim, error) {
	port := request.Port

	if other, found := current.ByPort(port); found {
		return Claim{}, fmt.Errorf("port %d is claimed by %s", port, other.Describe())
	}

	onPort := holdersOn(holders, port)
	for _, holder := range onPort {
		if !holder.belongsTo(request.ProjectDir, request.Project, request.PID) {
			return Claim{}, fmt.Errorf("port %d is in use by %s (pid %d)", port, holder.Process, holder.PID)
		}
	}
	if len(onPort) > 0 {
		return newClaim(request, port), nil
	}

	available, err := checker.Available(port)
	if err != nil {
		return Claim{}, fmt.Errorf("checking whether port %d is free: %w", port, err)
	}
	if !available {
		return Claim{}, fmt.Errorf("port %d cannot be bound. A process portly cannot see may hold it, or a recent connection may still be closing", port)
	}
	return newClaim(request, port), nil
}

// claimFirstFree claims the lowest port in request.Range that is unclaimed,
// has no listener, and can be bound.
func claimFirstFree(current Registry, holders []Holder, checker BindChecker, request Request) (Claim, error) {
	claimed := make(map[uint32]bool, len(current.Claims))
	for _, claim := range current.Claims {
		claimed[claim.Port] = true
	}
	listening := make(map[uint32]bool, len(holders))
	for _, holder := range holders {
		listening[holder.Port] = true
	}

	var claimedCount, listeningCount, unbindableCount int
	for port := request.Range.First; port <= request.Range.Last; port++ {
		if claimed[port] {
			claimedCount++
			continue
		}
		if listening[port] {
			listeningCount++
			continue
		}

		available, err := checker.Available(port)
		if err != nil {
			return Claim{}, fmt.Errorf("checking whether port %d is free: %w", port, err)
		}
		if !available {
			unbindableCount++
			continue
		}
		return newClaim(request, port), nil
	}

	return Claim{}, fmt.Errorf("no free port in %s: %d claimed, %d listening, %d could not be bound",
		request.Range, claimedCount, listeningCount, unbindableCount)
}

// newClaim builds the claim a request produces on port.
func newClaim(request Request, port uint32) Claim {
	return Claim{
		Port:         port,
		ProjectDir:   request.ProjectDir,
		Project:      request.Project,
		Name:         request.Name,
		RegisteredAt: request.Now.UTC(),
		PID:          request.PID,
	}
}

// holdersOn returns the holders bound to port.
func holdersOn(holders []Holder, port uint32) []Holder {
	var matched []Holder
	for _, holder := range holders {
		if holder.Port == port {
			matched = append(matched, holder)
		}
	}
	return matched
}
