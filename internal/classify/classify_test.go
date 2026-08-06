package classify

import (
	"testing"

	"github.com/velvet-tiger/portly/internal/scan"
)

// fakeDirectories answers existence questions from a fixed set of paths, so
// classification rules can be tested without a real filesystem.
type fakeDirectories struct {
	present map[string]bool
}

func (f fakeDirectories) Exists(path string) bool { return f.present[path] }

const testHome = "/Users/dev"

func TestClassify(t *testing.T) {
	directories := fakeDirectories{present: map[string]bool{
		"/Users/dev/code/shop/package.json":                                                  true,
		"/Users/dev/Library/Application Support/JetBrains/plugins/cline/1.1.64/package.json": true,
	}}

	cases := []struct {
		name          string
		listener      scan.Listener
		wantRelevance Relevance
		// wantProject is the project name expected, or empty when none should be
		// attributed. Project attribution is independent of relevance: it records
		// where a process is running, which stays factual even for a row that is
		// filtered out as an application.
		wantProject string
		wantAgent   string
	}{
		{
			name: "a container port is a dev server named by its container",
			listener: scan.Listener{
				Port:      8000,
				Container: &scan.Container{Name: "api-1", ComposeProject: "shop"},
				Process: scan.Process{
					Name:       "com.docker.backend",
					Executable: "/Applications/Docker.app/Contents/MacOS/com.docker.backend",
				},
			},
			wantRelevance: RelevanceDevServer,
		},
		{
			name: "an agent in the parent chain outranks everything else",
			listener: scan.Listener{
				Port: 5173,
				Process: scan.Process{
					Name:       "node",
					Executable: "/Users/dev/.nvm/versions/node/v24.0.0/bin/node",
					Arguments:  []string{"node", "/Users/dev/code/shop/node_modules/.bin/vite"},
					WorkingDir: scan.KnownDirectory("/Users/dev/code/shop"),
					Ancestry: []scan.Ancestor{
						{PID: 900, Name: "zsh"},
						{PID: 800, Name: "claude"},
					},
				},
			},
			wantRelevance: RelevanceDevServer,
			wantProject:   "shop",
			wantAgent:     "claude",
		},
		{
			name: "an editor's own port is an application even inside a project",
			listener: scan.Listener{
				Port: 63342,
				Process: scan.Process{
					Name:       "pycharm",
					Executable: "/Applications/PyCharm.app/Contents/MacOS/pycharm",
					WorkingDir: scan.KnownDirectory("/Users/dev/code/shop"),
				},
			},
			wantRelevance: RelevanceApplication,
			// The editor really is running in that directory, so the attribution
			// stands. What matters is that it is not promoted to a dev server.
			wantProject: "shop",
		},
		{
			name: "a plugin's bundled node is installed software, not a project",
			listener: scan.Listener{
				Port: 26040,
				Process: scan.Process{
					Name:       "node",
					Executable: "/Users/dev/Library/Application Support/JetBrains/plugins/cline/1.1.64/node",
					WorkingDir: scan.KnownDirectory("/Users/dev/Library/Application Support/JetBrains/plugins/cline/1.1.64"),
					Ancestry:   []scan.Ancestor{{PID: 700, Name: "webstorm"}},
				},
			},
			wantRelevance: RelevanceApplication,
		},
		{
			name: "a system daemon is recognised by its path",
			listener: scan.Listener{
				Port: 49152,
				Process: scan.Process{
					Name:       "rapportd",
					Executable: "/usr/libexec/rapportd",
					WorkingDir: scan.KnownDirectory("/"),
				},
			},
			wantRelevance: RelevanceSystem,
		},
		{
			name: "a dev runtime inside a project is a dev server without any agent",
			listener: scan.Listener{
				Port: 3000,
				Process: scan.Process{
					Name:       "node",
					Executable: "/opt/homebrew/bin/node",
					Arguments:  []string{"node", "server.js"},
					WorkingDir: scan.KnownDirectory("/Users/dev/code/shop"),
					ParentPID:  1,
				},
			},
			wantRelevance: RelevanceDevServer,
			wantProject:   "shop",
		},
		{
			name: "an unexplained port is not claimed to be noise",
			listener: scan.Listener{
				Port: 9000,
				Process: scan.Process{
					Name:       "mystery",
					Executable: "/opt/homebrew/bin/mystery",
					WorkingDir: scan.KnownDirectory("/opt/homebrew/var"),
				},
			},
			wantRelevance: RelevanceUnattributed,
		},
		{
			name: "a denied working directory is reported rather than assumed empty",
			listener: scan.Listener{
				Port: 4000,
				Process: scan.Process{
					Name:       "mystery",
					Executable: "/opt/mystery",
					WorkingDir: scan.UnknownDirectory(),
				},
			},
			wantRelevance: RelevanceUnattributed,
		},
	}

	classifier := NewClassifier(directories, testHome)

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := classifier.Classify(testCase.listener)

			if got.Relevance != testCase.wantRelevance {
				t.Errorf("Relevance = %v (%s), want %v", got.Relevance, got.Reason, testCase.wantRelevance)
			}
			if testCase.wantProject == "" && got.Project != nil {
				t.Errorf("Project = %q, want none", got.Project.Name)
			}
			if testCase.wantProject != "" {
				if got.Project == nil {
					t.Fatalf("Project = none, want %q", testCase.wantProject)
				}
				if got.Project.Name != testCase.wantProject {
					t.Errorf("Project = %q, want %q", got.Project.Name, testCase.wantProject)
				}
			}
			if got.Agent != testCase.wantAgent {
				t.Errorf("Agent = %q, want %q", got.Agent, testCase.wantAgent)
			}
			if got.Reason == "" {
				t.Error("Reason is empty; every classification must explain itself")
			}
		})
	}
}

func TestClassifyReportsADeniedWorkingDirectory(t *testing.T) {
	classifier := NewClassifier(fakeDirectories{}, testHome)

	result := classifier.Classify(scan.Listener{
		Port:    4000,
		Process: scan.Process{Name: "x", Executable: "/opt/x", WorkingDir: scan.UnknownDirectory()},
	})

	if result.Reason != "the OS would not report a working directory" {
		t.Errorf("Reason = %q, want the denied-directory explanation", result.Reason)
	}
}

func TestIsInstalledSoftware(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/Users/dev/Library/Application Support/x/node", true},
		{"/Users/dev/.cursor/extensions/y/server", true},
		{"/Users/dev/code/shop/server.js", false},
		{"/opt/homebrew/bin/node", false},
		{"/Users/other/Library/x", false},
	}

	for _, testCase := range cases {
		if got := IsInstalledSoftware(testCase.path, testHome); got != testCase.want {
			t.Errorf("IsInstalledSoftware(%q) = %v, want %v", testCase.path, got, testCase.want)
		}
	}
}

func TestIsInstalledSoftwareWithoutAHomeDirectory(t *testing.T) {
	if IsInstalledSoftware("/Users/dev/Library/x", "") {
		t.Error("with no home directory nothing can be identified as installed software")
	}
}
