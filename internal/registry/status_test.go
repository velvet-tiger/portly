package registry

import "testing"

func TestStatuses(t *testing.T) {
	tied := Claim{Port: 4004, ProjectDir: "/code/blog", Project: "blog", Name: "web", PID: 555}
	unbound := Claim{Port: 4005, ProjectDir: "/code/docs", Project: "docs", Name: "web"}

	current := Registry{}.With(webClaim()).With(apiClaim()).With(tied).With(unbound)
	holders := []Holder{
		{Port: 4000, PID: 10, Process: "node", ProjectDir: "/code/mockups"},
		{Port: 4001, PID: 11, Process: "python", ProjectDir: "/code/elsewhere"},
		{Port: 4002, PID: 12, Process: "ruby"},
		{Port: 4004, PID: 555, Process: "deno"},
	}
	binds := &fakeBinds{taken: map[uint32]bool{4005: true, 4007: true}}

	statuses, err := Statuses(current, holders, binds, PortRange{First: 4000, Last: 4007})
	if err != nil {
		t.Fatalf("Statuses() returned %v", err)
	}

	want := map[uint32]State{
		4000: StateActive,      // claimed, held by its own project
		4001: StateConflict,    // claimed, held by another project
		4002: StateListening,   // held, unclaimed
		4003: StateFree,        // nothing at all
		4004: StateActive,      // held by the claim's tied PID
		4005: StateConflict,    // claimed, invisible holder blocks the bind
		4006: StateFree,        // nothing at all
		4007: StateUnavailable, // unclaimed, invisible holder blocks the bind
	}
	if len(statuses) != len(want) {
		t.Fatalf("got %d statuses, want %d", len(statuses), len(want))
	}
	for _, status := range statuses {
		if status.State != want[status.Port] {
			t.Errorf("port %d is %q, want %q", status.Port, status.State, want[status.Port])
		}
	}
}

func TestStatusesOnlyTestBindsPortsWithNoVisibleHolder(t *testing.T) {
	holders := []Holder{{Port: 4000, PID: 10, Process: "node"}}
	binds := &fakeBinds{}

	if _, err := Statuses(Registry{}, holders, binds, PortRange{First: 4000, Last: 4001}); err != nil {
		t.Fatalf("Statuses() returned %v", err)
	}
	if len(binds.checked) != 1 || binds.checked[0] != 4001 {
		t.Errorf("checked binds %v, want only 4001", binds.checked)
	}
}

func TestStatusesRegisteredClaimWithNothingBound(t *testing.T) {
	statuses, err := Statuses(Registry{}.With(webClaim()), nil, &fakeBinds{}, PortRange{First: 4000, Last: 4000})
	if err != nil {
		t.Fatalf("Statuses() returned %v", err)
	}
	if statuses[0].State != StateRegistered {
		t.Errorf("state = %q, want registered", statuses[0].State)
	}
	if statuses[0].Claim == nil || statuses[0].Claim.Name != "web" {
		t.Errorf("claim = %+v, want web", statuses[0].Claim)
	}
}
