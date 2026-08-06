// Package classify decides what a listening port actually is.
//
// A developer machine holds far more listening ports than dev servers. The
// sample this package was designed against had 52 listeners, of which roughly
// ten were servers and the rest were editors, chat applications and OS daemons.
// Separating the two is the work; reading the socket table is the easy part.
package classify

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/velvet-tiger/portly/internal/scan"
)

// Relevance is what a listening port is for.
type Relevance int

const (
	// RelevanceUnattributed is a port portly could not explain. It is hidden by
	// default but is not claimed to be noise.
	//
	// It is the zero value deliberately. A Result that somehow skipped the rules
	// must not default to claiming the port is a dev server.
	RelevanceUnattributed Relevance = iota
	// RelevanceDevServer is a development server, which is what portly exists
	// to show.
	RelevanceDevServer
	// RelevanceApplication is a port opened by installed software: a GUI
	// application, or a bundled runtime shipped inside an editor plugin.
	RelevanceApplication
	// RelevanceSystem is an operating system daemon.
	RelevanceSystem
)

// String renders a Relevance for display.
func (r Relevance) String() string {
	switch r {
	case RelevanceDevServer:
		return "dev server"
	case RelevanceApplication:
		return "application"
	case RelevanceSystem:
		return "system"
	default:
		return "unattributed"
	}
}

// Result is everything portly concluded about one listener.
type Result struct {
	Relevance Relevance
	// Reason states which rule decided the classification, so a disputed result
	// can be argued with rather than merely disbelieved.
	Reason    string
	Runtime   Runtime
	Framework string
	// Project is the codebase the process is running in, when one was found.
	Project *Project
	// Agent is the coding agent that launched the process, when the parent chain
	// still reaches one.
	Agent string
}

// Classifier applies portly's relevance rules.
//
// It holds a DirectoryReader and a home directory rather than reaching for the
// filesystem and the environment directly, so its rules can be tested against a
// fabricated tree.
type Classifier struct {
	directories DirectoryReader
	homeDir     string
}

// NewClassifier builds a Classifier. homeDir bounds project attribution: a
// working directory outside it is treated as infrastructure rather than as
// somebody's code, which stops package manager trees such as a git-managed
// Homebrew prefix from being reported as projects.
func NewClassifier(directories DirectoryReader, homeDir string) *Classifier {
	return &Classifier{directories: directories, homeDir: homeDir}
}

// Classify decides what a listener is.
//
// Rules are applied in confidence order and the first match wins. Ordering
// carries real weight: the "process is itself a desktop application" rule sits
// above the working directory rules so that an editor running inside a project
// directory is still reported as an editor.
func (c *Classifier) Classify(listener scan.Listener) Result {
	process := listener.Process
	isContainer := listener.Container != nil

	result := Result{
		Relevance: RelevanceUnattributed,
		Reason:    "no rule matched",
		Runtime:   DetectRuntime(process.Executable, process.Name, process.Arguments, isContainer),
		Framework: DetectFramework(process.Arguments),
	}
	if project, found := c.findProject(process.WorkingDir); found {
		result.Project = &project
	}

	if isContainer {
		result.Relevance = RelevanceDevServer
		result.Reason = describeContainer(*listener.Container)
		return result
	}

	if ancestor, label, found := AgentAncestor(process.Ancestry); found {
		result.Relevance = RelevanceDevServer
		result.Agent = label
		result.Reason = fmt.Sprintf("launched by %s (pid %d)", label, ancestor.PID)
		return result
	}

	if IsDesktopApplication(process.Executable) {
		result.Relevance = RelevanceApplication
		result.Reason = fmt.Sprintf("%s is a desktop application", applicationName(process.Executable, process.Name))
		return result
	}

	// An editor plugin's bundled runtime is installed software, even though its
	// executable is an ordinary node or python binary in an ordinary directory.
	if IsInstalledSoftware(process.Executable, c.homeDir) {
		result.Relevance = RelevanceApplication
		result.Reason = "runs from installed software, not a project"
		return result
	}

	if IsSystemDaemon(process.Executable) {
		result.Relevance = RelevanceSystem
		result.Reason = "runs from a system directory"
		return result
	}

	if result.Runtime != RuntimeUnknown {
		if ancestor, label, found := SessionAncestor(process.Ancestry); found {
			result.Relevance = RelevanceDevServer
			result.Reason = fmt.Sprintf("%s started from %s (pid %d)", result.Runtime, label, ancestor.PID)
			return result
		}
		if result.Project != nil {
			result.Relevance = RelevanceDevServer
			result.Reason = fmt.Sprintf("%s running in %s", result.Runtime, result.Project.Name)
			return result
		}
	}

	if result.Framework != "" {
		result.Relevance = RelevanceDevServer
		result.Reason = fmt.Sprintf("%s server", result.Framework)
		return result
	}

	if !process.WorkingDir.Known {
		result.Reason = "the OS would not report a working directory"
	}
	return result
}

// findProject resolves a working directory to a project root inside the user's
// home directory.
func (c *Classifier) findProject(directory scan.WorkingDirectory) (Project, bool) {
	if !directory.Known {
		return Project{}, false
	}
	if !c.withinHome(directory.Path) {
		return Project{}, false
	}
	// Installed software carries its own manifests. Attributing a project to one
	// produces a name taken from a version directory.
	if IsInstalledSoftware(directory.Path, c.homeDir) {
		return Project{}, false
	}

	project, found := FindProject(c.directories, directory.Path)
	if !found || !c.withinHome(project.Root) {
		return Project{}, false
	}
	return project, true
}

// withinHome reports whether path sits inside the configured home directory.
func (c *Classifier) withinHome(path string) bool {
	if c.homeDir == "" {
		return false
	}
	relative, err := filepath.Rel(c.homeDir, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// describeContainer states which container holds a published port.
func describeContainer(container scan.Container) string {
	if container.ComposeProject != "" {
		return fmt.Sprintf("container %s in compose project %s", container.Name, container.ComposeProject)
	}
	return fmt.Sprintf("container %s", container.Name)
}

// applicationName recovers a readable application name from a bundle path,
// falling back to the process name when the path is not a bundle.
func applicationName(executable, processName string) string {
	lowered := strings.ToLower(executable)
	index := strings.Index(lowered, ".app/contents/")
	if index < 0 {
		return processName
	}
	return filepath.Base(executable[:index])
}
