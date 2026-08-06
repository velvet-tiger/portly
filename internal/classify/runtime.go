package classify

import (
	"path/filepath"
	"strings"
)

// Runtime is the language or engine executing a listening process.
type Runtime string

const (
	RuntimeUnknown   Runtime = "unknown"
	RuntimeNode      Runtime = "node"
	RuntimeDeno      Runtime = "deno"
	RuntimeBun       Runtime = "bun"
	RuntimePython    Runtime = "python"
	RuntimePHP       Runtime = "php"
	RuntimeRuby      Runtime = "ruby"
	RuntimeJava      Runtime = "java"
	RuntimeDotNet    Runtime = "dotnet"
	RuntimeContainer Runtime = "docker"
)

// runtimeByExecutable maps an executable's base name to its runtime.
//
// Names are matched after stripping a version suffix, so python3.12 and php8.4
// resolve without an entry each.
var runtimeByExecutable = map[string]Runtime{
	"node":    RuntimeNode,
	"nodejs":  RuntimeNode,
	"deno":    RuntimeDeno,
	"bun":     RuntimeBun,
	"python":  RuntimePython,
	"pypy":    RuntimePython,
	"php":     RuntimePHP,
	"php-fpm": RuntimePHP,
	"ruby":    RuntimeRuby,
	"java":    RuntimeJava,
	"dotnet":  RuntimeDotNet,
}

// frameworkByArgument maps a distinctive argv token to a framework label.
//
// Tokens are matched whole against argv entries and against the base name of
// path-like entries. That prevents a directory called "next" in an unrelated
// path from being read as Next.js.
var frameworkByArgument = map[string]string{
	"vite":               "vite",
	"next":               "next.js",
	"nuxt":               "nuxt",
	"astro":              "astro",
	"remix":              "remix",
	"webpack":            "webpack",
	"webpack-dev-server": "webpack",
	"react-scripts":      "create-react-app",
	"ng":                 "angular",
	"vue-cli-service":    "vue",
	"svelte-kit":         "sveltekit",
	"gatsby":             "gatsby",
	"nodemon":            "nodemon",
	"uvicorn":            "uvicorn",
	"gunicorn":           "gunicorn",
	"hypercorn":          "hypercorn",
	"flask":              "flask",
	"fastapi":            "fastapi",
	"manage.py":          "django",
	"artisan":            "laravel artisan",
	"rails":              "rails",
	"puma":               "puma",
	"unicorn":            "unicorn",
	"jekyll":             "jekyll",
	"hugo":               "hugo",
	"streamlit":          "streamlit",
	"jupyter":            "jupyter",
	"jupyter-lab":        "jupyter",
	"mkdocs":             "mkdocs",
	"storybook":          "storybook",
	"vitest":             "vitest",
	"serve":              "serve",
	"http-server":        "http-server",
}

// ambiguousFrameworkArguments are tokens that are also ordinary subcommands.
//
// "serve" names a real npm package and is also how `ollama serve` starts. They
// are matched only when they arrive as a path, such as node_modules/.bin/serve,
// which a subcommand never does.
var ambiguousFrameworkArguments = map[string]bool{
	"serve": true,
	"ng":    true,
	"next":  true,
}

// DetectRuntime identifies the engine behind a process.
//
// container reflects whether the port is published by a container, which takes
// precedence because the host-side process is Docker's own backend and says
// nothing about what runs inside.
func DetectRuntime(executable, name string, arguments []string, container bool) Runtime {
	if container {
		return RuntimeContainer
	}

	for _, candidate := range []string{filepath.Base(executable), name} {
		if runtime, found := runtimeByExecutable[stripVersionSuffix(candidate)]; found {
			return runtime
		}
	}

	// A wrapper such as `npx` or a shell exec leaves the engine in argv[0].
	if len(arguments) > 0 {
		if runtime, found := runtimeByExecutable[stripVersionSuffix(filepath.Base(arguments[0]))]; found {
			return runtime
		}
	}
	return RuntimeUnknown
}

// DetectFramework returns a framework label from argv, or an empty string.
//
// The first match wins in argv order, which favours the command actually being
// run over flags and paths that follow it.
func DetectFramework(arguments []string) string {
	for _, argument := range arguments {
		if framework, found := frameworkByArgument[argument]; found && !ambiguousFrameworkArguments[argument] {
			return framework
		}
		// Package binaries arrive as paths such as node_modules/.bin/vite.
		if base := filepath.Base(argument); base != argument {
			if framework, found := frameworkByArgument[base]; found {
				return framework
			}
		}
	}

	// PHP's built-in server is a flag rather than a named binary.
	if hasArgument(arguments, "-S") && looksLikePHP(arguments) {
		return "php built-in server"
	}
	return ""
}

// hasArgument reports whether argv contains an exact token.
func hasArgument(arguments []string, token string) bool {
	for _, argument := range arguments {
		if argument == token {
			return true
		}
	}
	return false
}

// looksLikePHP reports whether argv[0] is a PHP binary.
func looksLikePHP(arguments []string) bool {
	if len(arguments) == 0 {
		return false
	}
	return strings.HasPrefix(stripVersionSuffix(filepath.Base(arguments[0])), "php")
}

// stripVersionSuffix removes a trailing version from an executable name, so
// python3.12, php8.4 and node22 reduce to python, php and node.
func stripVersionSuffix(name string) string {
	trimmed := strings.TrimRight(name, "0123456789.")
	if trimmed == "" {
		return name
	}
	return trimmed
}
