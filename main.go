// Command portly reports the dev servers running on this machine.
//
// Most listening ports on a developer machine are not dev servers. portly
// reads the socket table, works out what is behind each port, and by default
// shows only the dev servers. It never hides rows silently: every run reports
// how many were filtered and why.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/velvet-tiger/portly/internal/classify"
	"github.com/velvet-tiger/portly/internal/docker"
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

func main() {
	settings, err := parseOptions(os.Args[1:])
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
