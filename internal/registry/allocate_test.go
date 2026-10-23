package registry

import (
	"errors"
	"strings"
	"testing"
)

func webRequest() Request {
	return Request{
		ProjectDir: "/code/mockups",
		Project:    "mockups",
		Name:       "web",
		Range:      PortRange{First: 4000, Last: 4009},
		Now:        registeredAt,
	}
}

func TestAllocatePicksTheLowestFreePort(t *testing.T) {
	claim, err := Allocate(Registry{}, nil, &fakeBinds{}, webRequest())
	if err != nil {
		t.Fatalf("Allocate() returned %v", err)
	}
	if claim.Port != 4000 {
		t.Errorf("port = %d, want 4000", claim.Port)
	}
	if claim.Name != "web" || claim.ProjectDir != "/code/mockups" || !claim.RegisteredAt.Equal(registeredAt) {
		t.Errorf("claim = %+v, does not carry the request", claim)
	}
}

func TestAllocateSkipsClaimedListeningAndUnbindablePorts(t *testing.T) {
	current := Registry{}.With(apiClaim()) // holds 4001
	other := Claim{Port: 4000, ProjectDir: "/code/blog", Project: "blog", Name: "web"}
	current = current.With(other)
	holders := []Holder{{Port: 4002, PID: 77, Process: "node"}}
	binds := &fakeBinds{taken: map[uint32]bool{4003: true}}

	claim, err := Allocate(current, holders, binds, webRequest())
	if err != nil {
		t.Fatalf("Allocate() returned %v", err)
	}
	if claim.Port != 4004 {
		t.Errorf("port = %d, want 4004", claim.Port)
	}
}

func TestAllocateReturnsTheExistingClaimForTheSameProjectAndName(t *testing.T) {
	existing := webClaim()
	existing.Port = 4007
	binds := &fakeBinds{}

	claim, err := Allocate(Registry{}.With(existing), nil, binds, webRequest())
	if err != nil {
		t.Fatalf("Allocate() returned %v", err)
	}
	if claim.Port != 4007 {
		t.Errorf("port = %d, want the existing 4007", claim.Port)
	}
	if len(binds.checked) != 0 {
		t.Errorf("checked binds %v, want none for an existing claim", binds.checked)
	}
}

func TestAllocateUpdatesThePIDOnAnExistingClaim(t *testing.T) {
	request := webRequest()
	request.PID = 4242

	claim, err := Allocate(Registry{}.With(webClaim()), nil, &fakeBinds{}, request)
	if err != nil {
		t.Fatalf("Allocate() returned %v", err)
	}
	if claim.PID != 4242 {
		t.Errorf("PID = %d, want 4242", claim.PID)
	}
}

func TestAllocateRefusesToMoveAnExistingClaim(t *testing.T) {
	request := webRequest()
	request.Port = 4500

	_, err := Allocate(Registry{}.With(webClaim()), nil, &fakeBinds{}, request)
	if err == nil || !strings.Contains(err.Error(), "already holds port 4000") {
		t.Fatalf("Allocate() returned %v, want an error naming the held port", err)
	}
}

func TestAllocateExactPortOutsideTheRange(t *testing.T) {
	request := webRequest()
	request.Port = 5174

	claim, err := Allocate(Registry{}, nil, &fakeBinds{}, request)
	if err != nil {
		t.Fatalf("Allocate() returned %v", err)
	}
	if claim.Port != 5174 {
		t.Errorf("port = %d, want 5174", claim.Port)
	}
}

func TestAllocateExactPortClaimedByAnotherProject(t *testing.T) {
	request := webRequest()
	request.Port = 4001

	_, err := Allocate(Registry{}.With(apiClaim()), nil, &fakeBinds{}, request)
	if err == nil || !strings.Contains(err.Error(), "api in shop") {
		t.Fatalf("Allocate() returned %v, want an error naming api in shop", err)
	}
}

func TestAllocateExactPortHeldByAnotherProject(t *testing.T) {
	request := webRequest()
	request.Port = 4003
	holders := []Holder{{Port: 4003, PID: 88, Process: "python", ProjectDir: "/code/shop"}}

	_, err := Allocate(Registry{}, holders, &fakeBinds{}, request)
	if err == nil || !strings.Contains(err.Error(), "python (pid 88)") {
		t.Fatalf("Allocate() returned %v, want an error naming the holder", err)
	}
}

func TestAllocateExactPortHeldByAnUnknownProject(t *testing.T) {
	request := webRequest()
	request.Port = 4003
	holders := []Holder{{Port: 4003, PID: 88, Process: "python"}}

	if _, err := Allocate(Registry{}, holders, &fakeBinds{}, request); err == nil {
		t.Fatal("Allocate() returned nil error for a port held by an unattributed process, want one")
	}
}

func TestAllocateExactPortAlreadyHeldByTheSameProject(t *testing.T) {
	request := webRequest()
	request.Port = 5174
	holders := []Holder{{Port: 5174, PID: 99, Process: "node", ProjectDir: "/code/mockups"}}
	binds := &fakeBinds{taken: map[uint32]bool{5174: true}}

	claim, err := Allocate(Registry{}, holders, binds, request)
	if err != nil {
		t.Fatalf("Allocate() returned %v", err)
	}
	if claim.Port != 5174 {
		t.Errorf("port = %d, want 5174", claim.Port)
	}
}

func TestAllocateExactPortHeldByAComposeProjectOfTheSameName(t *testing.T) {
	request := webRequest()
	request.Port = 5432
	holders := []Holder{{Port: 5432, PID: 13546, Process: "mockups-db-1", ProjectName: "mockups"}}

	if _, err := Allocate(Registry{}, holders, &fakeBinds{}, request); err != nil {
		t.Fatalf("Allocate() returned %v, want the compose project to count as the same project", err)
	}
}

func TestAllocateExactPortThatCannotBeBound(t *testing.T) {
	request := webRequest()
	request.Port = 4003
	binds := &fakeBinds{taken: map[uint32]bool{4003: true}}

	_, err := Allocate(Registry{}, nil, binds, request)
	if err == nil || !strings.Contains(err.Error(), "cannot be bound") {
		t.Fatalf("Allocate() returned %v, want a bind failure", err)
	}
}

func TestAllocateReportsAFullRange(t *testing.T) {
	request := webRequest()
	request.Range = PortRange{First: 4000, Last: 4001}
	holders := []Holder{{Port: 4000, PID: 1, Process: "node"}}
	binds := &fakeBinds{taken: map[uint32]bool{4001: true}}

	_, err := Allocate(Registry{}, holders, binds, request)
	if err == nil || !strings.Contains(err.Error(), "no free port in 4000-4001: 0 claimed, 1 listening, 1 could not be bound") {
		t.Fatalf("Allocate() returned %v, want a full-range error with counts", err)
	}
}

func TestAllocatePassesOnABindCheckFailure(t *testing.T) {
	binds := &fakeBinds{err: errors.New("socket table exploded")}

	_, err := Allocate(Registry{}, nil, binds, webRequest())
	if err == nil || !strings.Contains(err.Error(), "socket table exploded") {
		t.Fatalf("Allocate() returned %v, want the bind check error", err)
	}
}

func TestAllocateRejectsAnInvalidRequest(t *testing.T) {
	cases := map[string]func(*Request){
		"no name":          func(r *Request) { r.Name = "" },
		"relative project": func(r *Request) { r.ProjectDir = "code/mockups" },
		"no project name":  func(r *Request) { r.Project = "" },
		"empty range":      func(r *Request) { r.Range = PortRange{} },
		"inverted range":   func(r *Request) { r.Range = PortRange{First: 10, Last: 5} },
	}

	for name, breakRequest := range cases {
		t.Run(name, func(t *testing.T) {
			request := webRequest()
			breakRequest(&request)
			if _, err := Allocate(Registry{}, nil, &fakeBinds{}, request); err == nil {
				t.Fatal("Allocate() returned nil error, want one")
			}
		})
	}
}
