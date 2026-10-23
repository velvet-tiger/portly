package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/velvet-tiger/portly/internal/classify"
	"github.com/velvet-tiger/portly/internal/registry"
	"github.com/velvet-tiger/portly/internal/render"
	"github.com/velvet-tiger/portly/internal/scan"
)

// memoryStore keeps the registry in memory and counts saves.
type memoryStore struct {
	current registry.Registry
	saves   int
}

func (m *memoryStore) Read() (registry.Registry, error) { return m.current, nil }

func (m *memoryStore) Update(change func(registry.Registry) (registry.Registry, error)) error {
	next, err := change(m.current)
	if err != nil {
		return err
	}
	m.current = next
	m.saves++
	return nil
}

// openBinds reports every port bindable except those listed as taken.
type openBinds struct {
	taken map[uint32]bool
}

func (o openBinds) Available(port uint32) (bool, error) { return !o.taken[port], nil }

// runningProcesses reports the listed PIDs as running.
type runningProcesses struct {
	pids map[int32]bool
}

func (r runningProcesses) Running(pid int32) bool { return r.pids[pid] }

var claimTime = time.Date(2026, 10, 23, 14, 30, 0, 0, time.UTC)

func testInputs(store *memoryStore, existingDirs ...string) claimInputs {
	present := make(map[string]bool, len(existingDirs))
	for _, directory := range existingDirs {
		present[directory] = true
	}
	return claimInputs{
		store:       store,
		binds:       openBinds{},
		directories: fixedDirectoryReader{present: present},
		processes:   runningProcesses{},
	}
}

func mockupsRequest() registry.Request {
	return registry.Request{
		ProjectDir: "/Users/dev/code/mockups",
		Project:    "mockups",
		Name:       "web",
		Range:      registry.PortRange{First: 4000, Last: 4009},
		Now:        claimTime,
	}
}

func TestParseRegisterOptionsDefaults(t *testing.T) {
	settings, err := parseRegisterOptions([]string{"--name", "web"})
	if err != nil {
		t.Fatalf("parseRegisterOptions() returned %v", err)
	}
	if settings.name != "web" || settings.port != 0 || settings.portRange != registry.DefaultRange {
		t.Errorf("settings = %+v, want name web, no port, the default range", settings)
	}
}

func TestParseRegisterOptionsReadsEverything(t *testing.T) {
	settings, err := parseRegisterOptions([]string{
		"--name", "api", "--project", "../shop", "--range", "8000-8099", "--pid", "4242", "--json", "8001",
	})
	if err != nil {
		t.Fatalf("parseRegisterOptions() returned %v", err)
	}
	want := registerOptions{
		name:       "api",
		projectDir: "../shop",
		port:       8001,
		portRange:  registry.PortRange{First: 8000, Last: 8099},
		pid:        4242,
		asJSON:     true,
	}
	if settings != want {
		t.Errorf("settings = %+v, want %+v", settings, want)
	}
}

func TestParseRegisterOptionsRejectsBadInput(t *testing.T) {
	cases := map[string][]string{
		"missing name":     {"4000"},
		"bad port":         {"--name", "web", "70000"},
		"two ports":        {"--name", "web", "4000", "4001"},
		"bad range":        {"--name", "web", "--range", "4999-4000"},
		"negative pid":     {"--name", "web", "--pid", "-1"},
		"pid out of range": {"--name", "web", "--pid", "9999999999"},
	}
	for name, arguments := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRegisterOptions(arguments); err == nil {
				t.Fatalf("parseRegisterOptions(%v) returned nil error, want one", arguments)
			}
		})
	}
}

func TestParseRegisterOptionsHelpReturnsFlagErrHelp(t *testing.T) {
	if _, err := parseRegisterOptions([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseRegisterOptions([--help]) returned %v, want flag.ErrHelp", err)
	}
}

func TestParseReleaseOptions(t *testing.T) {
	byPort, err := parseReleaseOptions([]string{"4001"})
	if err != nil || byPort.port != 4001 {
		t.Errorf("parseReleaseOptions([4001]) = %+v, %v, want port 4001", byPort, err)
	}

	byName, err := parseReleaseOptions([]string{"--project", "/code/shop", "--name", "api"})
	if err != nil || byName.name != "api" || byName.projectDir != "/code/shop" {
		t.Errorf("parseReleaseOptions(--name) = %+v, %v, want name api in /code/shop", byName, err)
	}
}

func TestParseReleaseOptionsRejectsBadInput(t *testing.T) {
	cases := map[string][]string{
		"nothing":              nil,
		"port and name":        {"--name", "web", "4000"},
		"project without name": {"--project", "/code/shop", "4000"},
		"bad port":             {"0"},
	}
	for name, arguments := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseReleaseOptions(arguments); err == nil {
				t.Fatalf("parseReleaseOptions(%v) returned nil error, want one", arguments)
			}
		})
	}
}

func TestParsePortsOptions(t *testing.T) {
	settings, err := parsePortsOptions([]string{"--range", "3000-3099", "--free", "--json"})
	if err != nil {
		t.Fatalf("parsePortsOptions() returned %v", err)
	}
	if settings.portRange != (registry.PortRange{First: 3000, Last: 3099}) || !settings.freeOnly || !settings.asJSON {
		t.Errorf("settings = %+v, want range 3000-3099, free, json", settings)
	}

	if _, err := parsePortsOptions([]string{"4000"}); err == nil {
		t.Error("parsePortsOptions([4000]) returned nil error, want one")
	}
}

func TestResolveProjectWalksUpFromTheWorkingDirectory(t *testing.T) {
	directories := fixedDirectoryReader{present: map[string]bool{"/Users/dev/code/mockups/.git": true}}

	project, err := resolveProject("", "/Users/dev/code/mockups/src", directories)
	if err != nil {
		t.Fatalf("resolveProject() returned %v", err)
	}
	if project.Root != "/Users/dev/code/mockups" || project.Name != "mockups" {
		t.Errorf("project = %+v, want mockups", project)
	}
}

func TestResolveProjectUsesAnExplicitDirectoryAsGiven(t *testing.T) {
	directories := fixedDirectoryReader{present: map[string]bool{"/Users/dev/code/shop": true}}

	project, err := resolveProject("../shop", "/Users/dev/code/mockups", directories)
	if err != nil {
		t.Fatalf("resolveProject() returned %v", err)
	}
	if project.Root != "/Users/dev/code/shop" || project.Name != "shop" {
		t.Errorf("project = %+v, want /Users/dev/code/shop", project)
	}
}

func TestResolveProjectRejectsAMissingProject(t *testing.T) {
	directories := fixedDirectoryReader{present: map[string]bool{}}

	if _, err := resolveProject("", "/tmp/scratch", directories); err == nil {
		t.Error("resolveProject() with no marker returned nil error, want one")
	}
	if _, err := resolveProject("/gone", "/tmp", directories); err == nil {
		t.Error("resolveProject() with a missing explicit directory returned nil error, want one")
	}
}

func TestHoldersFromAttributesProjectsAndContainers(t *testing.T) {
	directories := fixedDirectoryReader{present: map[string]bool{"/Users/dev/code/mockups/.git": true}}
	classifier := classify.NewClassifier(directories, "/Users/dev")
	listeners := []scan.Listener{
		{Port: 5174, Process: scan.Process{PID: 10, Name: "node", WorkingDir: scan.KnownDirectory("/Users/dev/code/mockups")}},
		{Port: 5432, Process: scan.Process{PID: 11, Name: "com.docker.backend"},
			Container: &scan.Container{Name: "shop-db-1", ComposeProject: "shop"}},
		{Port: 7000, Process: scan.Process{PID: 12, Name: "ControlCenter"}},
	}

	holders := holdersFrom(listeners, classifier)

	want := []registry.Holder{
		{Port: 5174, PID: 10, Process: "node", ProjectDir: "/Users/dev/code/mockups", ProjectName: "mockups"},
		{Port: 5432, PID: 11, Process: "shop-db-1", ProjectName: "shop"},
		{Port: 7000, PID: 12, Process: "ControlCenter"},
	}
	if len(holders) != len(want) {
		t.Fatalf("got %d holders, want %d", len(holders), len(want))
	}
	for i := range want {
		if holders[i] != want[i] {
			t.Errorf("holder %d = %+v, want %+v", i, holders[i], want[i])
		}
	}
}

func TestRegisterClaimStoresAndReturnsTheClaim(t *testing.T) {
	store := &memoryStore{}
	var notices bytes.Buffer

	claim, err := registerClaim(mockupsRequest(), nil, testInputs(store, "/Users/dev/code/mockups"), &notices)
	if err != nil {
		t.Fatalf("registerClaim() returned %v", err)
	}
	if claim.Port != 4000 {
		t.Errorf("port = %d, want 4000", claim.Port)
	}
	if stored, found := store.current.ByPort(4000); !found || stored.Name != "web" {
		t.Errorf("store holds %+v, want the web claim on 4000", store.current.Claims)
	}
	if notices.Len() != 0 {
		t.Errorf("notices = %q, want none", notices.String())
	}
}

func TestRegisterClaimIsIdempotent(t *testing.T) {
	store := &memoryStore{}
	inputs := testInputs(store, "/Users/dev/code/mockups")

	first, err := registerClaim(mockupsRequest(), nil, inputs, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("first registerClaim() returned %v", err)
	}
	second, err := registerClaim(mockupsRequest(), nil, inputs, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("second registerClaim() returned %v", err)
	}

	if first != second {
		t.Errorf("second claim %+v differs from first %+v", second, first)
	}
	if len(store.current.Claims) != 1 {
		t.Errorf("store holds %d claims, want 1", len(store.current.Claims))
	}
}

func TestRegisterClaimReusesAStaleClaimsPortAndSaysSo(t *testing.T) {
	stale := registry.Claim{Port: 4000, ProjectDir: "/Users/dev/code/deleted", Project: "deleted", Name: "web", RegisteredAt: claimTime}
	store := &memoryStore{current: registry.Registry{}.With(stale)}
	var notices bytes.Buffer

	claim, err := registerClaim(mockupsRequest(), nil, testInputs(store, "/Users/dev/code/mockups"), &notices)
	if err != nil {
		t.Fatalf("registerClaim() returned %v", err)
	}
	if claim.Port != 4000 {
		t.Errorf("port = %d, want the stale claim's 4000", claim.Port)
	}
	if !strings.Contains(notices.String(), "dropped stale claim web in deleted on port 4000") {
		t.Errorf("notices = %q, want the stale claim named", notices.String())
	}
}

func TestRegisterClaimLeavesTheStoreAloneOnFailure(t *testing.T) {
	other := registry.Claim{Port: 4000, ProjectDir: "/Users/dev/code/shop", Project: "shop", Name: "api", RegisteredAt: claimTime}
	store := &memoryStore{current: registry.Registry{}.With(other)}
	request := mockupsRequest()
	request.Port = 4000

	_, err := registerClaim(request, nil, testInputs(store, "/Users/dev/code/mockups", "/Users/dev/code/shop"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("registerClaim() for a port claimed by another project returned nil error, want one")
	}
	if store.saves != 0 {
		t.Errorf("store saved %d times, want 0", store.saves)
	}
}

func TestReleaseClaimByNameAndByPort(t *testing.T) {
	web := registry.Claim{Port: 4000, ProjectDir: "/Users/dev/code/mockups", Project: "mockups", Name: "web", RegisteredAt: claimTime}
	api := registry.Claim{Port: 4001, ProjectDir: "/Users/dev/code/shop", Project: "shop", Name: "api", RegisteredAt: claimTime}
	store := &memoryStore{current: registry.Registry{}.With(web).With(api)}
	inputs := testInputs(store, "/Users/dev/code/mockups", "/Users/dev/code/shop")
	mockups := classify.Project{Root: "/Users/dev/code/mockups", Name: "mockups"}

	released, err := releaseClaim(releaseOptions{name: "web"}, mockups, inputs, &bytes.Buffer{})
	if err != nil || released.Port != 4000 {
		t.Fatalf("releaseClaim(--name web) = %+v, %v, want port 4000", released, err)
	}
	released, err = releaseClaim(releaseOptions{port: 4001}, classify.Project{}, inputs, &bytes.Buffer{})
	if err != nil || released.Name != "api" {
		t.Fatalf("releaseClaim(4001) = %+v, %v, want api", released, err)
	}
	if len(store.current.Claims) != 0 {
		t.Errorf("store holds %+v, want nothing", store.current.Claims)
	}
}

func TestReleaseClaimCanReleaseAStaleClaimByName(t *testing.T) {
	gone := registry.Claim{Port: 4000, ProjectDir: "/Users/dev/code/deleted", Project: "deleted", Name: "web", RegisteredAt: claimTime}
	store := &memoryStore{current: registry.Registry{}.With(gone)}
	project := classify.Project{Root: "/Users/dev/code/deleted", Name: "deleted"}

	if _, err := releaseClaim(releaseOptions{name: "web"}, project, testInputs(store), &bytes.Buffer{}); err != nil {
		t.Fatalf("releaseClaim() of a stale claim returned %v, want success", err)
	}
}

func TestReleaseClaimReportsAMissingClaim(t *testing.T) {
	store := &memoryStore{}
	if _, err := releaseClaim(releaseOptions{port: 4000}, classify.Project{}, testInputs(store), &bytes.Buffer{}); err == nil {
		t.Fatal("releaseClaim() of an unclaimed port returned nil error, want one")
	}
}

func TestBuildPortsReportDoesNotWrite(t *testing.T) {
	live := registry.Claim{Port: 4000, ProjectDir: "/Users/dev/code/mockups", Project: "mockups", Name: "web", RegisteredAt: claimTime}
	outside := registry.Claim{Port: 5174, ProjectDir: "/Users/dev/code/mockups", Project: "mockups", Name: "vite", RegisteredAt: claimTime}
	gone := registry.Claim{Port: 4001, ProjectDir: "/Users/dev/code/deleted", Project: "deleted", Name: "web", RegisteredAt: claimTime}
	store := &memoryStore{current: registry.Registry{}.With(live).With(outside).With(gone)}
	holders := []registry.Holder{{Port: 4002, PID: 7, Process: "ruby"}}
	settings := portsOptions{portRange: registry.PortRange{First: 4000, Last: 4004}}

	report, err := buildPortsReport(settings, holders, testInputs(store, "/Users/dev/code/mockups"))
	if err != nil {
		t.Fatalf("buildPortsReport() returned %v", err)
	}

	if store.saves != 0 || len(store.current.Claims) != 3 {
		t.Errorf("store saved %d times and holds %d claims, want 0 saves and 3 claims", store.saves, len(store.current.Claims))
	}
	if len(report.StaleClaims) != 1 || report.StaleClaims[0].Claim.Port != 4001 {
		t.Errorf("stale claims = %+v, want only 4001", report.StaleClaims)
	}
	if report.ClaimsOutsideRange != 1 {
		t.Errorf("claims outside range = %d, want 1", report.ClaimsOutsideRange)
	}

	var table bytes.Buffer
	if err := render.NewPortsTable(render.PlainPalette()).Write(&table, report); err != nil {
		t.Fatalf("writing the table: %v", err)
	}
	for _, want := range []string{"registered", "web in mockups", "listening", "ruby (pid 7)",
		"2 used, 3 free in 4000-4004. 1 stale claim ignored. 1 claim outside this range."} {
		if !strings.Contains(table.String(), want) {
			t.Errorf("table lacks %q:\n%s", want, table.String())
		}
	}
}

func TestPortsReportFreeOnlyPrintsBarePorts(t *testing.T) {
	store := &memoryStore{}
	holders := []registry.Holder{{Port: 4001, PID: 7, Process: "ruby"}}
	settings := portsOptions{portRange: registry.PortRange{First: 4000, Last: 4002}, freeOnly: true}

	report, err := buildPortsReport(settings, holders, testInputs(store))
	if err != nil {
		t.Fatalf("buildPortsReport() returned %v", err)
	}

	var out bytes.Buffer
	if err := render.NewPortsTable(render.PlainPalette()).Write(&out, report); err != nil {
		t.Fatalf("writing free ports: %v", err)
	}
	if out.String() != "4000\n4002\n" {
		t.Errorf("output = %q, want bare free ports", out.String())
	}
}

func TestPortsReportJSON(t *testing.T) {
	live := registry.Claim{Port: 4000, ProjectDir: "/Users/dev/code/mockups", Project: "mockups", Name: "web", RegisteredAt: claimTime, PID: 42}
	store := &memoryStore{current: registry.Registry{}.With(live)}
	inputs := testInputs(store, "/Users/dev/code/mockups")
	inputs.processes = runningProcesses{pids: map[int32]bool{42: true}}
	settings := portsOptions{portRange: registry.PortRange{First: 4000, Last: 4001}}

	report, err := buildPortsReport(settings, nil, inputs)
	if err != nil {
		t.Fatalf("buildPortsReport() returned %v", err)
	}

	var out bytes.Buffer
	if err := render.WritePortsJSON(&out, report); err != nil {
		t.Fatalf("WritePortsJSON() returned %v", err)
	}

	var document struct {
		Range string `json:"range"`
		Used  int    `json:"used"`
		Free  int    `json:"free"`
		Ports []struct {
			Port  uint32 `json:"port"`
			State string `json:"state"`
			Claim *struct {
				Name string `json:"name"`
				PID  *int32 `json:"pid"`
			} `json:"claim"`
		} `json:"ports"`
	}
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if document.Range != "4000-4001" || document.Used != 1 || document.Free != 1 || len(document.Ports) != 1 {
		t.Fatalf("document = %+v, want one used port in 4000-4001", document)
	}
	port := document.Ports[0]
	if port.State != "registered" || port.Claim == nil || port.Claim.Name != "web" || port.Claim.PID == nil || *port.Claim.PID != 42 {
		t.Errorf("port = %+v, want web registered on 4000 with pid 42", port)
	}
}
