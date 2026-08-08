package classify

import (
	"path/filepath"
	"strings"

	"github.com/velvet-tiger/portly/internal/scan"
)

// agentNames are executables that are coding agents. A listening process
// descended from one of these was almost certainly started by that agent.
var agentNames = map[string]string{
	"claude":       "claude",
	"aider":        "aider",
	"codex":        "codex",
	"goose":        "goose",
	"opencode":     "opencode",
	"cline":        "cline",
	"cursor-agent": "cursor",
	"amp":          "amp",
}

// agentPathMarkers identify an agent from its install path, which catches
// bundled binaries whose process name is too generic to match on.
var agentPathMarkers = map[string]string{
	"claude-code":  "claude",
	"claude.app":   "claude",
	"cursor.app":   "cursor",
	"windsurf.app": "windsurf",
}

// sessionNames are editors, terminals and shells. A dev server descended from
// one of these belongs to somebody's working session, which is weaker evidence
// than an agent but still separates a real server from a background daemon.
var sessionNames = map[string]string{
	"zsh": "a shell", "bash": "a shell", "fish": "a shell", "sh": "a shell",
	"code": "vs code", "code helper": "vs code", "zed": "zed",
	"pycharm": "pycharm", "webstorm": "webstorm", "phpstorm": "phpstorm",
	"idea": "intellij", "goland": "goland", "rubymine": "rubymine",
	"iterm2": "iterm", "terminal": "terminal", "warp": "warp",
	"ghostty": "ghostty", "alacritty": "alacritty", "kitty": "kitty",
	"tmux": "tmux", "nvim": "neovim", "vim": "vim", "emacs": "emacs",
	"sublime_text": "sublime text",
}

// desktopApplicationMarkers identify a process that is itself a GUI
// application. Such a process's own ports are internal plumbing, not a dev
// server, however many of them it opens.
var desktopApplicationMarkers = []string{
	".app/contents/",
}

// installedSoftwareHomeSubtrees hold software installed into a home directory
// rather than code somebody is working on.
//
// Editor plugins ship their own bundled runtimes here. A JetBrains plugin's
// node binary running from Application Support with its own package.json
// otherwise reads as a node dev server inside a project named after the
// plugin's version number.
var installedSoftwareHomeSubtrees = []string{
	"library",
	"applications",
	".vscode",
	".cursor",
	".local/share",
	".cache",
}

// runtimeManagerHomeSubtrees hold interpreters installed by a version manager
// such as nvm or rustup. They count as installed software for project
// attribution, but an interpreter living here is the user's own toolchain:
// nearly every node process on an nvm machine runs from ~/.nvm, so the
// executable path says nothing about what it runs. Relevance is judged by the
// script instead.
var runtimeManagerHomeSubtrees = []string{
	".nvm",
	".cargo",
	".rustup",
	".local/share/mise",
}

// systemPathPrefixes identify operating system daemons.
var systemPathPrefixes = []string{
	"/system/",
	"/usr/libexec/",
	"/usr/sbin/",
	"/sbin/",
	"/usr/lib/systemd/",
	"/library/apple/",
	"/library/privilegedhelpertools/",
}

// AgentAncestor returns the first coding agent found walking outwards from a
// process, and whether one was found.
//
// This is portly's strongest signal, and also its most fragile: a server
// backgrounded from a shell that has since exited is reparented to PID 1, which
// severs the chain. Such a server is still found, but by working directory
// rather than by ancestry.
func AgentAncestor(ancestry []scan.Ancestor) (scan.Ancestor, string, bool) {
	for _, ancestor := range ancestry {
		if label, found := matchAgent(ancestor); found {
			return ancestor, label, true
		}
	}
	return scan.Ancestor{}, "", false
}

// SessionAncestor returns the first editor, terminal or shell in the chain.
func SessionAncestor(ancestry []scan.Ancestor) (scan.Ancestor, string, bool) {
	for _, ancestor := range ancestry {
		if label, found := sessionNames[normaliseName(ancestor.Name)]; found {
			return ancestor, label, true
		}
	}
	return scan.Ancestor{}, "", false
}

// matchAgent tests one ancestor against both the name and path agent lists.
func matchAgent(ancestor scan.Ancestor) (string, bool) {
	if label, found := agentNames[normaliseName(ancestor.Name)]; found {
		return label, true
	}

	executable := strings.ToLower(ancestor.Executable)
	for marker, label := range agentPathMarkers {
		if strings.Contains(executable, marker) {
			return label, true
		}
	}
	return "", false
}

// IsDesktopApplication reports whether an executable lives inside a macOS
// application bundle.
func IsDesktopApplication(executable string) bool {
	lowered := strings.ToLower(executable)
	for _, marker := range desktopApplicationMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// IsInstalledSoftware reports whether a path sits in a home directory subtree
// that holds installed software, including version-managed runtime trees.
//
// homeDir may be empty, in which case nothing matches, because without a home
// directory there is no way to tell an install path from a project path.
func IsInstalledSoftware(path, homeDir string) bool {
	return underHomeSubtree(path, homeDir, installedSoftwareHomeSubtrees) ||
		underHomeSubtree(path, homeDir, runtimeManagerHomeSubtrees)
}

// IsVersionManagedRuntime reports whether a path sits in a version manager's
// tree, such as nvm's. An executable there is the user's own toolchain rather
// than part of an application, so its location must not decide relevance.
func IsVersionManagedRuntime(path, homeDir string) bool {
	return underHomeSubtree(path, homeDir, runtimeManagerHomeSubtrees)
}

// underHomeSubtree reports whether path sits under any of the named subtrees
// of homeDir.
func underHomeSubtree(path, homeDir string, subtrees []string) bool {
	if path == "" || homeDir == "" {
		return false
	}

	relative, err := filepath.Rel(homeDir, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}

	lowered := strings.ToLower(filepath.ToSlash(relative))
	for _, subtree := range subtrees {
		if lowered == subtree || strings.HasPrefix(lowered, subtree+"/") {
			return true
		}
	}
	return false
}

// IsSystemDaemon reports whether an executable sits in an operating system
// directory reserved for daemons.
func IsSystemDaemon(executable string) bool {
	lowered := strings.ToLower(executable)
	for _, prefix := range systemPathPrefixes {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return false
}

// normaliseName lowercases a process name and strips any executable extension,
// so "Code Helper" and "node.exe" compare predictably.
func normaliseName(name string) string {
	lowered := strings.ToLower(strings.TrimSpace(name))
	return strings.TrimSuffix(lowered, filepath.Ext(lowered))
}
