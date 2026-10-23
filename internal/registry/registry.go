// Package registry records which ports apps have claimed, so two apps on one
// machine do not pick the same port.
//
// Every decision is a pure function over injected inputs. Reading and writing
// the claims file lives behind Store, and the operating system checks for
// bindability and process liveness live behind interfaces. Each rule can be
// tested without touching the filesystem, the network or a real process.
package registry

import (
	"fmt"
	"sort"
	"time"

	"github.com/velvet-tiger/portly/internal/classify"
)

// Claim is one port held by one app in one project.
type Claim struct {
	Port uint32
	// ProjectDir is the absolute project root. Together with Name it identifies
	// the claim. Project is only a display name, and two checkouts can share a
	// base name.
	ProjectDir   string
	Project      string
	Name         string
	RegisteredAt time.Time
	// PID, when non-zero, ties the claim to a process. The claim is stale once
	// that process has exited.
	PID int32
}

// identifiedBy reports whether the claim belongs to name in projectDir.
func (c Claim) identifiedBy(projectDir, name string) bool {
	return c.ProjectDir == projectDir && c.Name == name
}

// Describe names the claim for messages, such as "web in mockups".
func (c Claim) Describe() string {
	return fmt.Sprintf("%s in %s", c.Name, c.Project)
}

// Registry is every claim on the machine, ordered by port.
//
// Methods never modify the receiver. Each change returns a new Registry, so a
// caller can decide on a change and discard it without side effects.
type Registry struct {
	Claims []Claim
}

// ByIdentity returns the claim held by name in projectDir.
func (r Registry) ByIdentity(projectDir, name string) (Claim, bool) {
	for _, claim := range r.Claims {
		if claim.identifiedBy(projectDir, name) {
			return claim, true
		}
	}
	return Claim{}, false
}

// ByPort returns the claim on port.
func (r Registry) ByPort(port uint32) (Claim, bool) {
	for _, claim := range r.Claims {
		if claim.Port == port {
			return claim, true
		}
	}
	return Claim{}, false
}

// With returns a registry holding claim, replacing any claim with the same
// project and name.
func (r Registry) With(claim Claim) Registry {
	next := make([]Claim, 0, len(r.Claims)+1)
	for _, existing := range r.Claims {
		if !existing.identifiedBy(claim.ProjectDir, claim.Name) {
			next = append(next, existing)
		}
	}
	next = append(next, claim)
	return Registry{Claims: sortedByPort(next)}
}

// ReleaseByName returns a registry without the claim held by name in
// projectDir, and the claim that was removed.
func (r Registry) ReleaseByName(projectDir, name string) (Registry, Claim, error) {
	released, found := r.ByIdentity(projectDir, name)
	if !found {
		return r, Claim{}, fmt.Errorf("no claim named %q in %s", name, projectDir)
	}
	return r.without(released), released, nil
}

// ReleaseByPort returns a registry without the claim on port, and the claim
// that was removed.
func (r Registry) ReleaseByPort(port uint32) (Registry, Claim, error) {
	released, found := r.ByPort(port)
	if !found {
		return r, Claim{}, fmt.Errorf("port %d is not claimed", port)
	}
	return r.without(released), released, nil
}

// without returns a registry missing the claim with the same identity as
// removed.
func (r Registry) without(removed Claim) Registry {
	next := make([]Claim, 0, len(r.Claims))
	for _, claim := range r.Claims {
		if !claim.identifiedBy(removed.ProjectDir, removed.Name) {
			next = append(next, claim)
		}
	}
	return Registry{Claims: next}
}

// ProcessChecker reports whether a process is still running.
type ProcessChecker interface {
	Running(pid int32) bool
}

// StaleClaim is a claim Prune removed, with the reason it no longer holds.
type StaleClaim struct {
	Claim  Claim
	Reason string
}

// Prune splits the registry into claims that still hold and claims that do
// not. A claim is stale when its project directory no longer exists, or when
// it is tied to a process that has exited.
//
// A recycled PID keeps a PID-tied claim alive until that process exits too.
// The operating system offers no cheap way to tell the two processes apart.
func Prune(r Registry, directories classify.DirectoryReader, processes ProcessChecker) (Registry, []StaleClaim) {
	kept := make([]Claim, 0, len(r.Claims))
	var stale []StaleClaim

	for _, claim := range r.Claims {
		switch {
		case !directories.Exists(claim.ProjectDir):
			stale = append(stale, StaleClaim{Claim: claim, Reason: fmt.Sprintf("%s no longer exists", claim.ProjectDir)})
		case claim.PID != 0 && !processes.Running(claim.PID):
			stale = append(stale, StaleClaim{Claim: claim, Reason: fmt.Sprintf("process %d has exited", claim.PID)})
		default:
			kept = append(kept, claim)
		}
	}
	return Registry{Claims: kept}, stale
}

// sortedByPort orders claims by port so the registry file diffs cleanly and
// output is stable.
func sortedByPort(claims []Claim) []Claim {
	sort.Slice(claims, func(a, b int) bool { return claims[a].Port < claims[b].Port })
	return claims
}
