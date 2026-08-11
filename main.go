// Command portly reports the dev servers running on this machine.
//
// Most listening ports on a developer machine are not dev servers. portly
// reads the socket table, works out what is behind each port, and by default
// shows only the dev servers. It never hides rows silently: every run reports
// how many were filtered and why.
//
// `portly kill <port>` stops the process listening on a given port.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/velvet-tiger/portly/internal/classify"
	"github.com/velvet-tiger/portly/internal/docker"
	"github.com/velvet-tiger/portly/internal/kill"
	"github.com/velvet-tiger/portly/internal/probe"
	"github.com/velvet-tiger/portly/internal/release"
	"github.com/velvet-tiger/portly/internal/render"
	"github.com/velvet-tiger/portly/internal/scan"
)

// scanTimeout bounds a whole run. Reading the socket table and describing every
// process is fast, and a bound means a wedged process cannot hang the CLI.
const scanTimeout = 20 * time.Second

// version and commit are set at release time with
// -ldflags "-X main.version=... -X main.commit=...".
//
// They are package-level variables because the -X linker flag can only write to
// one. A build that does not set them falls back to the module metadata Go
// embeds, so `go install` builds still report their provenance.
var (
	version = release.DevelopmentVersion
	commit  = ""
)

// options are the command line settings for one run.
type options struct {
	all          bool
	asJSON       bool
	withProbe    bool
	probeTimeout time.Duration
	noColour     bool
	showVersion  bool
}

// killOptions are the command line settings for `portly kill`.
type killOptions struct {
	// port is uint32 to match scan.Listener.Port, though parseKillOptions
	// restricts it to the valid 1-65535 range.
	port  uint32
	force bool
}

func main() {
	arguments := os.Args[1:]

	if len(arguments) > 0 && arguments[0] == "kill" {
		mainKill(arguments[1:])
		return
	}
	mainScan(arguments)
}

// mainScan runs the default command: scan every listening port and report
// the dev servers among them.
func mainScan(arguments []string) {
	settings, err := parseOptions(arguments)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "portly: %v\n", err)
		os.Exit(2)
	}

	if settings.showVersion {
		info, _ := debug.ReadBuildInfo()
		fmt.Println(release.Describe(version, commit, info))
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()

	if err := run(ctx, settings, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "portly: %v\n", err)
		os.Exit(1)
	}
}

// mainKill runs `portly kill <port>`.
func mainKill(arguments []string) {
	settings, err := parseKillOptions(arguments)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "portly: %v\n", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()

	scanner := scan.NewScanner(scan.NewSocketTable(), scan.NewProcessTable(), docker.NewIndex())
	if err := runKill(ctx, settings, scanner, kill.OSSignaller{}, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "portly: %v\n", err)
		os.Exit(1)
	}
}

// parseOptions reads the command line.
func parseOptions(arguments []string) (options, error) {
	var settings options

	flags := flag.NewFlagSet("portly", flag.ContinueOnError)
	flags.BoolVar(&settings.all, "all", false,
		"show every listening port, including apps and system daemons")
	flags.BoolVar(&settings.asJSON, "json", false,
		"print JSON instead of a table")
	flags.BoolVar(&settings.withProbe, "probe", false,
		"send an HTTP GET to each port to see what it serves")
	flags.DurationVar(&settings.probeTimeout, "probe-timeout", 400*time.Millisecond,
		"how long each probe waits before giving up")
	flags.BoolVar(&settings.noColour, "no-color", false,
		"turn colour off")
	flags.BoolVar(&settings.showVersion, "version", false,
		"print the version and exit")

	flags.Usage = func() {
		fmt.Fprint(flags.Output(), "portly finds the dev servers running on this machine.\n\nUsage:\n  portly [flags]\n\nFlags:\n")
		flags.PrintDefaults()
	}

	if err := flags.Parse(arguments); err != nil {
		return options{}, err
	}
	if settings.probeTimeout <= 0 {
		return options{}, fmt.Errorf("--probe-timeout must be positive, got %s", settings.probeTimeout)
	}
	return settings, nil
}

// parseKillOptions reads the command line for `portly kill`.
func parseKillOptions(arguments []string) (killOptions, error) {
	var settings killOptions

	flags := flag.NewFlagSet("portly kill", flag.ContinueOnError)
	flags.BoolVar(&settings.force, "force", false,
		"send SIGKILL instead of SIGTERM")

	flags.Usage = func() {
		fmt.Fprint(flags.Output(), "portly kill stops the process listening on a port.\n\nUsage:\n  portly kill [flags] <port>\n\nFlags:\n")
		flags.PrintDefaults()
	}

	if err := flags.Parse(arguments); err != nil {
		return killOptions{}, err
	}

	remaining := flags.Args()
	if len(remaining) != 1 {
		return killOptions{}, fmt.Errorf("portly kill needs exactly one port, got %d", len(remaining))
	}

	port, err := strconv.ParseUint(remaining[0], 10, 16)
	if err != nil {
		return killOptions{}, fmt.Errorf("%q is not a valid port: %w", remaining[0], err)
	}
	if port == 0 {
		return killOptions{}, fmt.Errorf("%q is not a valid port: ports run from 1 to 65535", remaining[0])
	}

	settings.port = uint32(port)
	return settings, nil
}

// run performs one scan and writes the result.
func run(ctx context.Context, settings options, out *os.File) error {
	home, err := os.UserHomeDir()
	if err != nil {
		// Project attribution is bounded by the home directory. Without it,
		// portly still reports ports; it stops naming projects. Say so rather
		// than pretending the results are complete.
		fmt.Fprintf(os.Stderr, "portly: no home directory found, so the PROJECT column will be empty: %v\n", err)
		home = ""
	}

	scanner := scan.NewScanner(scan.NewSocketTable(), scan.NewProcessTable(), docker.NewIndex())
	listeners, err := scanner.Scan(ctx)
	if err != nil {
		return err
	}

	classifier := classify.NewClassifier(classify.OSDirectoryReader{}, home)
	rows, hidden := buildRows(listeners, classifier, settings.all)

	if settings.withProbe {
		attachProbes(ctx, rows, settings.probeTimeout)
	}

	now := time.Now()
	if settings.asJSON {
		return render.NewJSON(now).Write(out, rows, hidden)
	}

	palette := render.ColourPalette()
	if settings.noColour || !useColour(out) {
		palette = render.PlainPalette()
	}
	return render.NewTable(palette, now, settings.withProbe).Write(out, rows, hidden)
}

// buildRows classifies listeners and applies the default filter.
//
// It returns the rows to display and a count of what was filtered out, keyed by
// why. The caller always reports the second value, so filtering is visible.
func buildRows(listeners []scan.Listener, classifier *classify.Classifier, all bool) ([]render.Row, map[classify.Relevance]int) {
	rows := make([]render.Row, 0, len(listeners))
	hidden := make(map[classify.Relevance]int)

	for _, listener := range listeners {
		result := classifier.Classify(listener)

		if !all && result.Relevance != classify.RelevanceDevServer {
			hidden[result.Relevance]++
			continue
		}
		rows = append(rows, render.Row{Listener: listener, Result: result})
	}
	return rows, hidden
}

// attachProbes probes every displayed port and attaches the results in place.
func attachProbes(ctx context.Context, rows []render.Row, timeout time.Duration) {
	targets := make([]probe.Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, probe.Target{
			Port:      row.Listener.Port,
			Addresses: row.Listener.Addresses,
		})
	}

	results := probe.NewHTTPProbe(timeout).InspectAll(ctx, targets)

	for i := range rows {
		if result, found := results[rows[i].Listener.Port]; found {
			copied := result
			rows[i].Probe = &copied
		}
	}
}

// runKill scans for listeners on settings.port and signals what it finds.
func runKill(ctx context.Context, settings killOptions, scanner *scan.Scanner, signaller kill.Signaller, out io.Writer) error {
	listeners, err := scanner.Scan(ctx)
	if err != nil {
		return err
	}
	return killPort(settings, listeners, signaller, out)
}

// killPort signals every process listening on settings.port and reports what
// happened.
//
// It takes an already-scanned listener list rather than a scanner, so the
// targeting and signalling logic can be tested against a fabricated list, with
// no real scan or process involved.
func killPort(settings killOptions, listeners []scan.Listener, signaller kill.Signaller, out io.Writer) error {
	targets := selectKillTargets(listeners, settings.port)
	if len(targets) == 0 {
		return fmt.Errorf("no process is listening on port %d", settings.port)
	}

	killSignal := syscall.SIGTERM
	if settings.force {
		killSignal = syscall.SIGKILL
	}

	killed := 0
	for _, listener := range targets {
		if listener.Container != nil {
			// The process behind a published container port is Docker's own
			// backend, shared across every container. Signalling it would stop
			// far more than the one port asked for.
			fmt.Fprintf(out, "port %d is published by container %s. Stop it with: docker stop %s\n",
				settings.port, listener.Container.Name, listener.Container.Name)
			continue
		}

		pids := append([]int32{listener.Process.PID}, listener.SiblingPIDs...)
		for _, pid := range pids {
			if err := signaller.Signal(pid, killSignal); err != nil {
				fmt.Fprintf(out, "could not signal pid %d: %v\n", pid, err)
				continue
			}
			fmt.Fprintf(out, "killed %s (pid %d) on port %d\n", listener.Process.Name, pid, settings.port)
			killed++
		}
	}

	if killed == 0 {
		return fmt.Errorf("nothing on port %d could be killed", settings.port)
	}
	return nil
}

// selectKillTargets returns every listener bound to port.
//
// More than one is unusual but possible: SO_REUSEPORT lets unrelated processes
// share a port, and portly only merges such listeners when they share an
// executable name (see collapseWorkerPools in internal/scan).
func selectKillTargets(listeners []scan.Listener, port uint32) []scan.Listener {
	var targets []scan.Listener
	for _, listener := range listeners {
		if listener.Port == port {
			targets = append(targets, listener)
		}
	}
	return targets
}

// useColour reports whether the output stream should carry colour.
//
// lipgloss detects terminal capability, and NO_COLOR is honoured because it is
// the convention every other CLI on the machine follows.
func useColour(out *os.File) bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	return lipgloss.NewRenderer(out).ColorProfile() != 0
}
