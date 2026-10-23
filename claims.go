package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/velvet-tiger/portly/internal/classify"
	"github.com/velvet-tiger/portly/internal/docker"
	"github.com/velvet-tiger/portly/internal/registry"
	"github.com/velvet-tiger/portly/internal/render"
	"github.com/velvet-tiger/portly/internal/scan"
)

// registerOptions are the command line settings for `portly register`.
type registerOptions struct {
	name string
	// projectDir is the --project value as typed. Empty means the project is
	// found from the working directory.
	projectDir string
	port       uint32
	portRange  registry.PortRange
	pid        int32
	asJSON     bool
}

// releaseOptions are the command line settings for `portly release`.
type releaseOptions struct {
	name       string
	projectDir string
	port       uint32
}

// portsOptions are the command line settings for `portly ports`.
type portsOptions struct {
	portRange registry.PortRange
	freeOnly  bool
	asJSON    bool
	noColour  bool
}

// claimInputs are what the claim commands read from and write to the
// machine. Each is replaced with a fake under test.
type claimInputs struct {
	store       registry.Store
	binds       registry.BindChecker
	directories classify.DirectoryReader
	processes   registry.ProcessChecker
}

// mainRegister runs `portly register`.
func mainRegister(arguments []string) {
	settings, err := parseRegisterOptions(arguments)
	exitOnParseError(err)

	ctx, cancel := commandContext()
	defer cancel()

	if err := runRegister(ctx, settings, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "portly: %v\n", err)
		os.Exit(1)
	}
}

// mainRelease runs `portly release`.
func mainRelease(arguments []string) {
	settings, err := parseReleaseOptions(arguments)
	exitOnParseError(err)

	if err := runRelease(settings, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "portly: %v\n", err)
		os.Exit(1)
	}
}

// mainPorts runs `portly ports`.
func mainPorts(arguments []string) {
	settings, err := parsePortsOptions(arguments)
	exitOnParseError(err)

	ctx, cancel := commandContext()
	defer cancel()

	if err := runPorts(ctx, settings, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "portly: %v\n", err)
		os.Exit(1)
	}
}

// exitOnParseError exits for a command line that could not be parsed. Asking
// for help is a success.
func exitOnParseError(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	fmt.Fprintf(os.Stderr, "portly: %v\n", err)
	os.Exit(2)
}

// commandContext bounds a command by scanTimeout and cancels it on Ctrl-C.
func commandContext() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	return ctx, func() {
		cancel()
		stop()
	}
}

// parseRegisterOptions reads the command line for `portly register`.
func parseRegisterOptions(arguments []string) (registerOptions, error) {
	settings := registerOptions{portRange: registry.DefaultRange}
	var rangeText string
	var pid int64

	flags := flag.NewFlagSet("portly register", flag.ContinueOnError)
	flags.StringVar(&settings.name, "name", "",
		"name of the app claiming the port, unique within its project (required)")
	flags.StringVar(&settings.projectDir, "project", "",
		"project directory, instead of the project found from the working directory")
	flags.StringVar(&rangeText, "range", registry.DefaultRange.String(),
		"range to pick a free port from when no port is given")
	flags.Int64Var(&pid, "pid", 0,
		"tie the claim to this process, so it ends when the process exits")
	flags.BoolVar(&settings.asJSON, "json", false,
		"print the claim as JSON instead of the port number")

	flags.Usage = func() {
		fmt.Fprint(flags.Output(), "portly register claims a port for an app, so other apps do not take it.\n\nIt prints the claimed port. Registering the same name in the same project again\nreturns the same port.\n\nUsage:\n  portly register [flags] --name <name> [port]\n\nFlags:\n")
		flags.PrintDefaults()
	}

	if err := flags.Parse(arguments); err != nil {
		return registerOptions{}, err
	}
	if settings.name == "" {
		return registerOptions{}, fmt.Errorf("portly register needs --name")
	}
	if pid < 0 || pid > int64(^uint32(0)>>1) {
		return registerOptions{}, fmt.Errorf("--pid %d is not a valid process ID", pid)
	}
	settings.pid = int32(pid)

	portRange, err := registry.ParsePortRange(rangeText)
	if err != nil {
		return registerOptions{}, fmt.Errorf("--range: %w", err)
	}
	settings.portRange = portRange

	remaining := flags.Args()
	switch len(remaining) {
	case 0:
	case 1:
		port, err := registry.ParsePort(remaining[0])
		if err != nil {
			return registerOptions{}, err
		}
		settings.port = port
	default:
		return registerOptions{}, fmt.Errorf("portly register takes at most one port, got %d arguments", len(remaining))
	}
	return settings, nil
}

// parseReleaseOptions reads the command line for `portly release`.
func parseReleaseOptions(arguments []string) (releaseOptions, error) {
	var settings releaseOptions

	flags := flag.NewFlagSet("portly release", flag.ContinueOnError)
	flags.StringVar(&settings.name, "name", "",
		"release this app's claim in the current project")
	flags.StringVar(&settings.projectDir, "project", "",
		"project directory for --name, instead of the project found from the working directory")

	flags.Usage = func() {
		fmt.Fprint(flags.Output(), "portly release drops a port claim.\n\nUsage:\n  portly release <port>\n  portly release [--project <dir>] --name <name>\n\nFlags:\n")
		flags.PrintDefaults()
	}

	if err := flags.Parse(arguments); err != nil {
		return releaseOptions{}, err
	}

	remaining := flags.Args()
	if settings.name != "" {
		if len(remaining) != 0 {
			return releaseOptions{}, fmt.Errorf("portly release accepts either a port or --name, not both")
		}
		return settings, nil
	}
	if settings.projectDir != "" {
		return releaseOptions{}, fmt.Errorf("--project only applies with --name")
	}
	if len(remaining) != 1 {
		return releaseOptions{}, fmt.Errorf("portly release needs exactly one port or --name, got %d arguments", len(remaining))
	}

	port, err := registry.ParsePort(remaining[0])
	if err != nil {
		return releaseOptions{}, err
	}
	settings.port = port
	return settings, nil
}

// parsePortsOptions reads the command line for `portly ports`.
func parsePortsOptions(arguments []string) (portsOptions, error) {
	var settings portsOptions
	var rangeText string

	flags := flag.NewFlagSet("portly ports", flag.ContinueOnError)
	flags.StringVar(&rangeText, "range", registry.DefaultRange.String(),
		"range of ports to report on")
	flags.BoolVar(&settings.freeOnly, "free", false,
		"list free ports, one per line, instead of used ones")
	flags.BoolVar(&settings.asJSON, "json", false,
		"print JSON instead of a table")
	flags.BoolVar(&settings.noColour, "no-color", false,
		"turn colour off")

	flags.Usage = func() {
		fmt.Fprint(flags.Output(), "portly ports reports which ports in a range are claimed, listening or free.\n\nUsage:\n  portly ports [flags]\n\nFlags:\n")
		flags.PrintDefaults()
	}

	if err := flags.Parse(arguments); err != nil {
		return portsOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return portsOptions{}, fmt.Errorf("portly ports takes no arguments, got %d", len(flags.Args()))
	}

	portRange, err := registry.ParsePortRange(rangeText)
	if err != nil {
		return portsOptions{}, fmt.Errorf("--range: %w", err)
	}
	settings.portRange = portRange
	return settings, nil
}

// runRegister claims a port and prints it.
func runRegister(ctx context.Context, settings registerOptions, out, notices io.Writer) error {
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("finding the working directory: %w", err)
	}
	inputs, err := osClaimInputs()
	if err != nil {
		return err
	}
	project, err := resolveProject(settings.projectDir, workingDir, inputs.directories)
	if err != nil {
		return err
	}
	holders, err := scanHolders(ctx)
	if err != nil {
		return err
	}

	request := registry.Request{
		ProjectDir: project.Root,
		Project:    project.Name,
		Name:       settings.name,
		Port:       settings.port,
		Range:      settings.portRange,
		PID:        settings.pid,
		Now:        time.Now(),
	}
	claim, err := registerClaim(request, holders, inputs, notices)
	if err != nil {
		return err
	}

	if settings.asJSON {
		return render.WriteClaimJSON(out, claim)
	}
	_, err = fmt.Fprintln(out, claim.Port)
	return err
}

// runRelease drops a claim and reports which one.
func runRelease(settings releaseOptions, out, notices io.Writer) error {
	inputs, err := osClaimInputs()
	if err != nil {
		return err
	}

	var project classify.Project
	if settings.name != "" {
		workingDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("finding the working directory: %w", err)
		}
		project, err = resolveProject(settings.projectDir, workingDir, inputs.directories)
		if err != nil {
			return err
		}
	}

	released, err := releaseClaim(settings, project, inputs, notices)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "released port %d (%s)\n", released.Port, released.Describe())
	return err
}

// runPorts reports the state of every port in a range.
func runPorts(ctx context.Context, settings portsOptions, out *os.File, notices io.Writer) error {
	inputs, err := osClaimInputs()
	if err != nil {
		return err
	}
	holders, err := scanHolders(ctx)
	if err != nil {
		return err
	}

	report, err := buildPortsReport(settings, holders, inputs)
	if err != nil {
		return err
	}

	if settings.asJSON {
		return render.WritePortsJSON(out, report)
	}
	if settings.freeOnly {
		// Free ports print bare for piping, so stale claims are reported on
		// stderr instead of in a summary line.
		reportStaleClaims(notices, report.StaleClaims, "ignored")
	}

	palette := render.ColourPalette()
	if settings.noColour || !useColour(out) {
		palette = render.PlainPalette()
	}
	return render.NewPortsTable(palette).Write(out, report)
}

// osClaimInputs builds claimInputs backed by the real machine.
func osClaimInputs() (claimInputs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		// DefaultPath can still succeed from XDG_STATE_HOME. If it cannot,
		// its error names the missing home directory.
		home = ""
	}
	path, err := registry.DefaultPath(os.Getenv("XDG_STATE_HOME"), home)
	if err != nil {
		return claimInputs{}, err
	}

	return claimInputs{
		store:       registry.NewFileStore(path),
		binds:       registry.OSBindChecker{},
		directories: classify.OSDirectoryReader{},
		processes:   registry.OSProcessChecker{},
	}, nil
}

// scanHolders scans listening ports and reduces them to registry holders.
func scanHolders(ctx context.Context) ([]registry.Holder, error) {
	scanner := scan.NewScanner(scan.NewSocketTable(), scan.NewProcessTable(), docker.NewIndex())
	listeners, err := scanner.Scan(ctx)
	if err != nil {
		return nil, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot attribute listening ports to projects without a home directory: %w", err)
	}
	classifier := classify.NewClassifier(classify.OSDirectoryReader{}, home)
	return holdersFrom(listeners, classifier), nil
}

// holdersFrom reduces listeners to what claim decisions need. A container is
// attributed to its compose project, which is what portly displays for it.
func holdersFrom(listeners []scan.Listener, classifier *classify.Classifier) []registry.Holder {
	holders := make([]registry.Holder, 0, len(listeners))
	for _, listener := range listeners {
		holder := registry.Holder{
			Port:    listener.Port,
			PID:     listener.Process.PID,
			Process: listener.Process.Name,
		}

		if container := listener.Container; container != nil {
			holder.Process = container.Name
			holder.ProjectName = container.ComposeProject
		} else if project := classifier.Classify(listener).Project; project != nil {
			holder.ProjectDir = project.Root
			holder.ProjectName = project.Name
		}
		holders = append(holders, holder)
	}
	return holders
}

// resolveProject finds the project a claim belongs to.
//
// An explicit directory is used as given, made absolute against workingDir.
// Otherwise the project is found by walking up from workingDir, the same way
// the PROJECT column is filled.
func resolveProject(explicit, workingDir string, directories classify.DirectoryReader) (classify.Project, error) {
	if explicit != "" {
		root := explicit
		if !filepath.IsAbs(root) {
			root = filepath.Join(workingDir, root)
		}
		root = filepath.Clean(root)
		if !directories.Exists(root) {
			return classify.Project{}, fmt.Errorf("project directory %s does not exist", root)
		}
		return classify.Project{Root: root, Name: filepath.Base(root)}, nil
	}

	project, found := classify.FindProject(directories, workingDir)
	if !found {
		return classify.Project{}, fmt.Errorf("no project found at or above %s. Run portly inside a project or pass --project <dir>", workingDir)
	}
	return project, nil
}

// registerClaim prunes stale claims, then allocates and stores a claim.
//
// Stale claims are pruned first so the ports they held can be handed out.
// Each dropped claim is reported on notices once the registry is saved.
func registerClaim(request registry.Request, holders []registry.Holder, inputs claimInputs, notices io.Writer) (registry.Claim, error) {
	var claim registry.Claim
	var stale []registry.StaleClaim

	err := inputs.store.Update(func(current registry.Registry) (registry.Registry, error) {
		kept, dropped := registry.Prune(current, inputs.directories, inputs.processes)
		allocated, err := registry.Allocate(kept, holders, inputs.binds, request)
		if err != nil {
			return current, err
		}
		claim, stale = allocated, dropped
		return kept.With(allocated), nil
	})
	if err != nil {
		return registry.Claim{}, err
	}

	reportStaleClaims(notices, stale, "dropped")
	return claim, nil
}

// releaseClaim removes the requested claim, then prunes stale claims.
//
// Releasing comes first so a stale claim can still be released by name rather
// than vanishing and making the release fail.
func releaseClaim(settings releaseOptions, project classify.Project, inputs claimInputs, notices io.Writer) (registry.Claim, error) {
	var released registry.Claim
	var stale []registry.StaleClaim

	err := inputs.store.Update(func(current registry.Registry) (registry.Registry, error) {
		var next registry.Registry
		var removed registry.Claim
		var err error
		if settings.name != "" {
			next, removed, err = current.ReleaseByName(project.Root, settings.name)
		} else {
			next, removed, err = current.ReleaseByPort(settings.port)
		}
		if err != nil {
			return current, err
		}

		kept, dropped := registry.Prune(next, inputs.directories, inputs.processes)
		released, stale = removed, dropped
		return kept, nil
	})
	if err != nil {
		return registry.Claim{}, err
	}

	reportStaleClaims(notices, stale, "dropped")
	return released, nil
}

// buildPortsReport reads the registry and decides the state of every port in
// the requested range. It does not write: stale claims are left out of the
// report and listed, but stay in the file until a command that writes runs.
func buildPortsReport(settings portsOptions, holders []registry.Holder, inputs claimInputs) (render.PortsReport, error) {
	current, err := inputs.store.Read()
	if err != nil {
		return render.PortsReport{}, err
	}
	kept, stale := registry.Prune(current, inputs.directories, inputs.processes)

	statuses, err := registry.Statuses(kept, holders, inputs.binds, settings.portRange)
	if err != nil {
		return render.PortsReport{}, err
	}

	outside := 0
	for _, claim := range kept.Claims {
		if !settings.portRange.Contains(claim.Port) {
			outside++
		}
	}

	return render.PortsReport{
		Range:              settings.portRange,
		Statuses:           statuses,
		StaleClaims:        stale,
		ClaimsOutsideRange: outside,
		FreeOnly:           settings.freeOnly,
	}, nil
}

// reportStaleClaims names each stale claim, so none disappears silently.
func reportStaleClaims(notices io.Writer, stale []registry.StaleClaim, verb string) {
	for _, entry := range stale {
		fmt.Fprintf(notices, "portly: %s stale claim %s on port %d: %s\n",
			verb, entry.Claim.Describe(), entry.Claim.Port, entry.Reason)
	}
}
