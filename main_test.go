package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"strings"
	"syscall"
	"testing"

	"github.com/velvet-tiger/portly/internal/classify"
	"github.com/velvet-tiger/portly/internal/scan"
)

type fixedProcessTable struct {
	process scan.Process
	err     error
}

// fixedDirectoryReader answers project-marker lookups from a fabricated tree.
// It keeps project-targeted kills independent of the real filesystem.
type fixedDirectoryReader struct {
	present map[string]bool
}

func (f fixedDirectoryReader) Exists(path string) bool { return f.present[path] }

func (f fixedProcessTable) Describe(context.Context, int32) (scan.Process, error) {
	return f.process, f.err
}

func TestParseDirectoryOptionsReadsThePID(t *testing.T) {
	settings, err := parseDirectoryOptions([]string{"32219"})
	if err != nil {
		t.Fatalf("parseDirectoryOptions() returned %v", err)
	}
	if settings.pid != 32219 {
		t.Errorf("pid = %d, want 32219", settings.pid)
	}
}

func TestParseDirectoryOptionsRejectsMissingPID(t *testing.T) {
	if _, err := parseDirectoryOptions(nil); err == nil {
		t.Fatal("parseDirectoryOptions(nil) returned nil error, want one naming the missing PID")
	}
}

func TestParseDirectoryOptionsRejectsInvalidPID(t *testing.T) {
	if _, err := parseDirectoryOptions([]string{"not-a-pid"}); err == nil {
		t.Fatal("parseDirectoryOptions() with a non-numeric PID returned nil error, want one")
	}
}

func TestParseDirectoryOptionsRejectsNonPositivePID(t *testing.T) {
	for _, pid := range []string{"0", "-1"} {
		if _, err := parseDirectoryOptions([]string{pid}); err == nil {
			t.Errorf("parseDirectoryOptions(%q) returned nil error, want one", pid)
		}
	}
}

func TestParseDirectoryOptionsHelpReturnsFlagErrHelp(t *testing.T) {
	_, err := parseDirectoryOptions([]string{"--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseDirectoryOptions([--help]) returned %v, want flag.ErrHelp", err)
	}
}

func TestRunDirectoryWritesTheWorkingDirectory(t *testing.T) {
	var out bytes.Buffer
	processes := fixedProcessTable{process: scan.Process{
		PID:        32219,
		WorkingDir: scan.KnownDirectory("/Users/dev/code/web"),
	}}

	err := runDirectory(context.Background(), directoryOptions{pid: 32219}, processes, &out)
	if err != nil {
		t.Fatalf("runDirectory() returned %v", err)
	}
	if got, want := out.String(), "/Users/dev/code/web\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRunDirectoryReportsAnUnavailableWorkingDirectory(t *testing.T) {
	processes := fixedProcessTable{process: scan.Process{
		PID:        32219,
		WorkingDir: scan.UnknownDirectory(),
	}}

	err := runDirectory(context.Background(), directoryOptions{pid: 32219}, processes, &bytes.Buffer{})
	if err == nil {
		t.Fatal("runDirectory() returned nil error, want unavailable-directory error")
	}
	if !strings.Contains(err.Error(), "working directory for process 32219 is unavailable") {
		t.Errorf("error = %q, want unavailable-directory message", err)
	}
}

func TestRunDirectoryReturnsAProcessLookupError(t *testing.T) {
	wantErr := errors.New("no such process")
	processes := fixedProcessTable{err: wantErr}

	err := runDirectory(context.Background(), directoryOptions{pid: 32219}, processes, &bytes.Buffer{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("runDirectory() returned %v, want %v", err, wantErr)
	}
}

// fakeSignal records one call made to a fakeSignaller.
type fakeSignal struct {
	pid    int32
	signal syscall.Signal
}

// fakeSignaller records every call it receives, and fails for any PID present
// in errs, which models a process that has already exited.
type fakeSignaller struct {
	calls []fakeSignal
	errs  map[int32]error
}

func (f *fakeSignaller) Signal(pid int32, signal syscall.Signal) error {
	f.calls = append(f.calls, fakeSignal{pid: pid, signal: signal})
	return f.errs[pid]
}

func TestParseKillOptionsReadsThePort(t *testing.T) {
	settings, err := parseKillOptions([]string{"3000"})
	if err != nil {
		t.Fatalf("parseKillOptions() returned %v", err)
	}
	if settings.port != 3000 {
		t.Errorf("port = %d, want 3000", settings.port)
	}
	if settings.force {
		t.Error("force = true, want false by default")
	}
}

func TestParseKillOptionsReadsForce(t *testing.T) {
	settings, err := parseKillOptions([]string{"--force", "3000"})
	if err != nil {
		t.Fatalf("parseKillOptions() returned %v", err)
	}
	if !settings.force {
		t.Error("force = false, want true")
	}
}

func TestParseKillOptionsReadsProject(t *testing.T) {
	settings, err := parseKillOptions([]string{"--force", "--project", "shop"})
	if err != nil {
		t.Fatalf("parseKillOptions() returned %v", err)
	}
	if settings.project != "shop" {
		t.Errorf("project = %q, want shop", settings.project)
	}
	if !settings.force {
		t.Error("force = false, want true")
	}
}

func TestParseKillOptionsRejectsPortAndProjectTogether(t *testing.T) {
	if _, err := parseKillOptions([]string{"--project", "shop", "3000"}); err == nil {
		t.Fatal("parseKillOptions() with a port and --project returned nil error, want one")
	}
}

func TestParseKillOptionsRejectsMissingPort(t *testing.T) {
	if _, err := parseKillOptions(nil); err == nil {
		t.Fatal("parseKillOptions(nil) returned nil error, want one naming the missing port")
	}
}

func TestParseKillOptionsRejectsExtraArguments(t *testing.T) {
	if _, err := parseKillOptions([]string{"3000", "4000"}); err == nil {
		t.Fatal("parseKillOptions() with two ports returned nil error, want one")
	}
}

func TestParseKillOptionsRejectsANonNumericPort(t *testing.T) {
	if _, err := parseKillOptions([]string{"not-a-port"}); err == nil {
		t.Fatal("parseKillOptions() with a non-numeric port returned nil error, want one")
	}
}

func TestParseKillOptionsRejectsPortZero(t *testing.T) {
	if _, err := parseKillOptions([]string{"0"}); err == nil {
		t.Fatal("parseKillOptions() with port 0 returned nil error, want one")
	}
}

func TestParseKillOptionsRejectsAnOutOfRangePort(t *testing.T) {
	if _, err := parseKillOptions([]string{"70000"}); err == nil {
		t.Fatal("parseKillOptions() with port 70000 returned nil error, want one")
	}
}

func TestParseKillOptionsHelpReturnsFlagErrHelp(t *testing.T) {
	_, err := parseKillOptions([]string{"--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseKillOptions([--help]) returned %v, want flag.ErrHelp", err)
	}
}

func TestSelectKillTargetsMatchesByPort(t *testing.T) {
	listeners := []scan.Listener{
		{Port: 3000, Process: scan.Process{PID: 1}},
		{Port: 4000, Process: scan.Process{PID: 2}},
	}

	targets := selectKillTargets(listeners, 3000)
	if len(targets) != 1 || targets[0].Process.PID != 1 {
		t.Fatalf("got %v, want the single listener on port 3000", targets)
	}
}

func TestSelectKillTargetsReturnsNoneForAnUnusedPort(t *testing.T) {
	listeners := []scan.Listener{{Port: 3000, Process: scan.Process{PID: 1}}}

	if targets := selectKillTargets(listeners, 9999); len(targets) != 0 {
		t.Errorf("got %d targets, want none for a port with no listener", len(targets))
	}
}

func TestKillPortSignalsTheListeningProcess(t *testing.T) {
	listeners := []scan.Listener{
		{Port: 3000, Process: scan.Process{PID: 42, Name: "node"}},
	}
	signaller := &fakeSignaller{}
	var out bytes.Buffer

	if err := killPort(killOptions{port: 3000}, listeners, signaller, &out); err != nil {
		t.Fatalf("killPort() returned %v", err)
	}

	if len(signaller.calls) != 1 {
		t.Fatalf("got %d signal calls, want 1", len(signaller.calls))
	}
	if signaller.calls[0] != (fakeSignal{pid: 42, signal: syscall.SIGTERM}) {
		t.Errorf("got %+v, want a SIGTERM to pid 42", signaller.calls[0])
	}
	if !strings.Contains(out.String(), "killed node (pid 42) on port 3000") {
		t.Errorf("output %q does not report the kill", out.String())
	}
}

func TestKillPortForceSendsSIGKILL(t *testing.T) {
	listeners := []scan.Listener{{Port: 3000, Process: scan.Process{PID: 42, Name: "node"}}}
	signaller := &fakeSignaller{}

	if err := killPort(killOptions{port: 3000, force: true}, listeners, signaller, &bytes.Buffer{}); err != nil {
		t.Fatalf("killPort() returned %v", err)
	}

	if signaller.calls[0].signal != syscall.SIGKILL {
		t.Errorf("signal = %v, want SIGKILL", signaller.calls[0].signal)
	}
}

func TestKillPortSignalsEverySiblingInAWorkerPool(t *testing.T) {
	listeners := []scan.Listener{
		{Port: 9000, Process: scan.Process{PID: 1, Name: "php-fpm"}, SiblingPIDs: []int32{2, 3}},
	}
	signaller := &fakeSignaller{}

	if err := killPort(killOptions{port: 9000}, listeners, signaller, &bytes.Buffer{}); err != nil {
		t.Fatalf("killPort() returned %v", err)
	}

	if len(signaller.calls) != 3 {
		t.Fatalf("got %d signal calls, want 3 (the master plus two siblings)", len(signaller.calls))
	}
}

func TestKillPortRefusesAContainerPublishedPort(t *testing.T) {
	listeners := []scan.Listener{
		{
			Port:      5432,
			Process:   scan.Process{PID: 1, Name: "com.docker.backend"},
			Container: &scan.Container{Name: "shop-db-1"},
		},
	}
	signaller := &fakeSignaller{}
	var out bytes.Buffer

	err := killPort(killOptions{port: 5432}, listeners, signaller, &out)
	if err == nil {
		t.Fatal("killPort() on a container port returned nil error, want one since nothing was signalled")
	}
	if len(signaller.calls) != 0 {
		t.Errorf("got %d signal calls, want 0; a container's host process must not be signalled directly", len(signaller.calls))
	}
	if !strings.Contains(out.String(), "docker stop shop-db-1") {
		t.Errorf("output %q does not point at docker stop", out.String())
	}
}

func TestKillPortReportsButSurvivesAnAlreadyExitedSibling(t *testing.T) {
	listeners := []scan.Listener{
		{Port: 9000, Process: scan.Process{PID: 1, Name: "php-fpm"}, SiblingPIDs: []int32{2}},
	}
	signaller := &fakeSignaller{errs: map[int32]error{2: errors.New("no such process")}}
	var out bytes.Buffer

	if err := killPort(killOptions{port: 9000}, listeners, signaller, &out); err != nil {
		t.Fatalf("killPort() returned %v, want nil since the master was still signalled", err)
	}
	if !strings.Contains(out.String(), "could not signal pid 2") {
		t.Errorf("output %q does not report the failed sibling", out.String())
	}
}

func TestKillPortReturnsAnErrorForAnUnusedPort(t *testing.T) {
	err := killPort(killOptions{port: 3000}, nil, &fakeSignaller{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("killPort() with no listeners returned nil error, want one")
	}
}

func TestSelectProjectKillTargetsMatchesNativeAndComposeProjects(t *testing.T) {
	listeners := []scan.Listener{
		{Port: 3000, Process: scan.Process{PID: 42, WorkingDir: scan.KnownDirectory("/Users/dev/code/shop/api")}},
		{Port: 4000, Process: scan.Process{PID: 43, WorkingDir: scan.KnownDirectory("/Users/dev/code/other")}},
		{Port: 5432, Process: scan.Process{PID: 44}, Container: &scan.Container{Name: "shop-db-1", ComposeProject: "shop"}},
	}
	classifier := classify.NewClassifier(fixedDirectoryReader{present: map[string]bool{
		"/Users/dev/code/shop/.git":          true,
		"/Users/dev/code/other/package.json": true,
	}}, "/Users/dev")

	targets := selectProjectKillTargets(listeners, "shop", classifier)
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want the native and compose listeners for shop", len(targets))
	}
	if targets[0].Process.PID != 42 || targets[1].Process.PID != 44 {
		t.Errorf("targets = %+v, want the shop listeners", targets)
	}
}

func TestKillProjectSignalsEveryUniqueNativeProcess(t *testing.T) {
	listeners := []scan.Listener{
		{Port: 3000, Process: scan.Process{PID: 42, Name: "node", WorkingDir: scan.KnownDirectory("/Users/dev/code/shop")}},
		{Port: 3001, Process: scan.Process{PID: 42, Name: "node", WorkingDir: scan.KnownDirectory("/Users/dev/code/shop")}},
		{Port: 9000, Process: scan.Process{PID: 43, Name: "php-fpm", WorkingDir: scan.KnownDirectory("/Users/dev/code/shop")}, SiblingPIDs: []int32{44}},
	}
	classifier := classify.NewClassifier(fixedDirectoryReader{present: map[string]bool{
		"/Users/dev/code/shop/.git": true,
	}}, "/Users/dev")
	signaller := &fakeSignaller{}
	var out bytes.Buffer

	err := killProject(killOptions{project: "shop"}, listeners, classifier, signaller, &out)
	if err != nil {
		t.Fatalf("killProject() returned %v", err)
	}
	if got, want := len(signaller.calls), 3; got != want {
		t.Fatalf("got %d signal calls, want %d (one server plus a worker pool)", got, want)
	}
	for _, call := range signaller.calls {
		if call.signal != syscall.SIGTERM {
			t.Errorf("signal for pid %d = %v, want SIGTERM", call.pid, call.signal)
		}
	}
	if !strings.Contains(out.String(), "killed node (pid 42) in project shop") {
		t.Errorf("output %q does not identify the project-targeted kill", out.String())
	}
}

func TestKillProjectReportsContainerButDoesNotSignalIt(t *testing.T) {
	listeners := []scan.Listener{{
		Port:      5432,
		Process:   scan.Process{PID: 1, Name: "com.docker.backend"},
		Container: &scan.Container{Name: "shop-db-1", ComposeProject: "shop"},
	}}
	signaller := &fakeSignaller{}
	var out bytes.Buffer

	err := killProject(killOptions{project: "shop"}, listeners, classify.NewClassifier(fixedDirectoryReader{}, "/Users/dev"), signaller, &out)
	if err == nil {
		t.Fatal("killProject() returned nil error, want one because no process was safe to signal")
	}
	if len(signaller.calls) != 0 {
		t.Errorf("got %d signal calls, want none for a published container port", len(signaller.calls))
	}
	if !strings.Contains(out.String(), "docker stop shop-db-1") {
		t.Errorf("output %q does not point at docker stop", out.String())
	}
}
