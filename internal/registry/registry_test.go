package registry

import (
	"testing"
	"time"
)

// fakeDirectories answers existence checks from a fabricated set of paths.
type fakeDirectories struct {
	present map[string]bool
}

func (f fakeDirectories) Exists(path string) bool { return f.present[path] }

// fakeProcesses answers liveness checks from a fabricated set of PIDs.
type fakeProcesses struct {
	running map[int32]bool
}

func (f fakeProcesses) Running(pid int32) bool { return f.running[pid] }

// fakeBinds reports every port bindable except those listed as taken.
type fakeBinds struct {
	taken   map[uint32]bool
	checked []uint32
	err     error
}

func (f *fakeBinds) Available(port uint32) (bool, error) {
	f.checked = append(f.checked, port)
	if f.err != nil {
		return false, f.err
	}
	return !f.taken[port], nil
}

var registeredAt = time.Date(2026, 10, 23, 14, 30, 0, 0, time.UTC)

func webClaim() Claim {
	return Claim{Port: 4000, ProjectDir: "/code/mockups", Project: "mockups", Name: "web", RegisteredAt: registeredAt}
}

func apiClaim() Claim {
	return Claim{Port: 4001, ProjectDir: "/code/shop", Project: "shop", Name: "api", RegisteredAt: registeredAt}
}

func TestWithKeepsClaimsOrderedByPort(t *testing.T) {
	registry := Registry{}.With(apiClaim()).With(webClaim())

	if len(registry.Claims) != 2 {
		t.Fatalf("got %d claims, want 2", len(registry.Claims))
	}
	if registry.Claims[0].Port != 4000 || registry.Claims[1].Port != 4001 {
		t.Errorf("ports = %d, %d, want 4000, 4001", registry.Claims[0].Port, registry.Claims[1].Port)
	}
}

func TestWithReplacesTheClaimWithTheSameProjectAndName(t *testing.T) {
	moved := webClaim()
	moved.Port = 4500

	registry := Registry{}.With(webClaim()).With(moved)

	if len(registry.Claims) != 1 {
		t.Fatalf("got %d claims, want 1", len(registry.Claims))
	}
	if registry.Claims[0].Port != 4500 {
		t.Errorf("port = %d, want 4500", registry.Claims[0].Port)
	}
}

func TestWithDoesNotModifyTheReceiver(t *testing.T) {
	original := Registry{Claims: []Claim{apiClaim()}}
	_ = original.With(webClaim())

	if len(original.Claims) != 1 {
		t.Errorf("original has %d claims after With, want 1", len(original.Claims))
	}
}

func TestReleaseByNameRemovesOnlyThatClaim(t *testing.T) {
	registry := Registry{}.With(webClaim()).With(apiClaim())

	next, released, err := registry.ReleaseByName("/code/mockups", "web")
	if err != nil {
		t.Fatalf("ReleaseByName() returned %v", err)
	}
	if released.Port != 4000 {
		t.Errorf("released port %d, want 4000", released.Port)
	}
	if len(next.Claims) != 1 || next.Claims[0].Name != "api" {
		t.Errorf("remaining claims = %+v, want only api", next.Claims)
	}
	if len(registry.Claims) != 2 {
		t.Errorf("original has %d claims after release, want 2", len(registry.Claims))
	}
}

func TestReleaseByNameNeedsTheSameProject(t *testing.T) {
	registry := Registry{}.With(webClaim())

	if _, _, err := registry.ReleaseByName("/code/shop", "web"); err == nil {
		t.Fatal("ReleaseByName() for another project's name returned nil error, want one")
	}
}

func TestReleaseByPortRemovesTheClaim(t *testing.T) {
	registry := Registry{}.With(webClaim()).With(apiClaim())

	next, released, err := registry.ReleaseByPort(4001)
	if err != nil {
		t.Fatalf("ReleaseByPort() returned %v", err)
	}
	if released.Name != "api" {
		t.Errorf("released %q, want api", released.Name)
	}
	if len(next.Claims) != 1 {
		t.Errorf("got %d claims after release, want 1", len(next.Claims))
	}
}

func TestReleaseByPortRejectsAnUnclaimedPort(t *testing.T) {
	if _, _, err := (Registry{}).ReleaseByPort(4000); err == nil {
		t.Fatal("ReleaseByPort() on an unclaimed port returned nil error, want one")
	}
}

func TestPruneDropsClaimsWhoseProjectIsGone(t *testing.T) {
	registry := Registry{}.With(webClaim()).With(apiClaim())
	directories := fakeDirectories{present: map[string]bool{"/code/shop": true}}

	kept, stale := Prune(registry, directories, fakeProcesses{})

	if len(kept.Claims) != 1 || kept.Claims[0].Name != "api" {
		t.Errorf("kept = %+v, want only api", kept.Claims)
	}
	if len(stale) != 1 || stale[0].Claim.Name != "web" {
		t.Fatalf("stale = %+v, want only web", stale)
	}
	if stale[0].Reason == "" {
		t.Error("stale claim has no reason")
	}
}

func TestPruneDropsClaimsTiedToAnExitedProcess(t *testing.T) {
	tied := webClaim()
	tied.PID = 4242
	directories := fakeDirectories{present: map[string]bool{"/code/mockups": true}}

	kept, stale := Prune(Registry{}.With(tied), directories, fakeProcesses{})

	if len(kept.Claims) != 0 {
		t.Errorf("kept = %+v, want none", kept.Claims)
	}
	if len(stale) != 1 {
		t.Errorf("got %d stale claims, want 1", len(stale))
	}
}

func TestPruneKeepsClaimsTiedToARunningProcess(t *testing.T) {
	tied := webClaim()
	tied.PID = 4242
	directories := fakeDirectories{present: map[string]bool{"/code/mockups": true}}
	processes := fakeProcesses{running: map[int32]bool{4242: true}}

	kept, stale := Prune(Registry{}.With(tied), directories, processes)

	if len(kept.Claims) != 1 || len(stale) != 0 {
		t.Errorf("kept %d and dropped %d, want 1 kept and 0 dropped", len(kept.Claims), len(stale))
	}
}

func TestParsePortRange(t *testing.T) {
	cases := []struct {
		text    string
		want    PortRange
		wantErr bool
	}{
		{text: "4000-4999", want: PortRange{First: 4000, Last: 4999}},
		{text: "8080-8080", want: PortRange{First: 8080, Last: 8080}},
		{text: "4000", wantErr: true},
		{text: "4999-4000", wantErr: true},
		{text: "0-10", wantErr: true},
		{text: "1-65536", wantErr: true},
		{text: "a-b", wantErr: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.text, func(t *testing.T) {
			got, err := ParsePortRange(testCase.text)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("ParsePortRange(%q) returned nil error, want one", testCase.text)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePortRange(%q) returned %v", testCase.text, err)
			}
			if got != testCase.want {
				t.Errorf("ParsePortRange(%q) = %+v, want %+v", testCase.text, got, testCase.want)
			}
		})
	}
}
