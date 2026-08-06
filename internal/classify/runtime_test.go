package classify

import "testing"

func TestDetectRuntime(t *testing.T) {
	cases := []struct {
		name       string
		executable string
		process    string
		arguments  []string
		container  bool
		want       Runtime
	}{
		{
			name:       "a container port reports the container runtime, not the docker backend",
			executable: "/Applications/Docker.app/Contents/MacOS/com.docker.backend",
			process:    "com.docker.backend",
			container:  true,
			want:       RuntimeContainer,
		},
		{
			name:       "a versioned python binary resolves to python",
			executable: "/opt/homebrew/bin/python3.12",
			process:    "python3.12",
			want:       RuntimePython,
		},
		{
			name:       "php-fpm keeps its hyphen and still resolves to php",
			executable: "/opt/homebrew/opt/php@8.4/sbin/php-fpm",
			process:    "php-fpm",
			want:       RuntimePHP,
		},
		{
			name:       "argv[0] identifies the engine when the executable does not",
			executable: "",
			process:    "sh",
			arguments:  []string{"node", "server.js"},
			want:       RuntimeNode,
		},
		{
			name:       "an unrecognised binary is not guessed at",
			executable: "/usr/libexec/rapportd",
			process:    "rapportd",
			want:       RuntimeUnknown,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := DetectRuntime(testCase.executable, testCase.process, testCase.arguments, testCase.container)
			if got != testCase.want {
				t.Errorf("DetectRuntime() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestDetectFramework(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
		want      string
	}{
		{
			name:      "a package binary path identifies the framework",
			arguments: []string{"node", "/app/node_modules/.bin/vite"},
			want:      "vite",
		},
		{
			name:      "an ordinary subcommand named serve is not a framework",
			arguments: []string{"/Applications/Ollama.app/Contents/Resources/ollama", "serve"},
			want:      "",
		},
		{
			name:      "the serve package is still detected when it arrives as a path",
			arguments: []string{"node", "/app/node_modules/.bin/serve", "-p", "3000"},
			want:      "serve",
		},
		{
			name:      "php's built-in server is detected from its flag",
			arguments: []string{"/opt/homebrew/bin/php8.4", "-S", "localhost:8000"},
			want:      "php built-in server",
		},
		{
			name:      "the -S flag alone does not imply php",
			arguments: []string{"/usr/bin/ssh", "-S", "/tmp/socket"},
			want:      "",
		},
		{
			name:      "laravel artisan is detected",
			arguments: []string{"php", "artisan", "serve"},
			want:      "laravel artisan",
		},
		{
			name:      "no arguments yields no framework",
			arguments: nil,
			want:      "",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := DetectFramework(testCase.arguments); got != testCase.want {
				t.Errorf("DetectFramework() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestStripVersionSuffix(t *testing.T) {
	cases := map[string]string{
		"python3.12": "python",
		"php8.4":     "php",
		"node":       "node",
		"php-fpm":    "php-fpm",
		"123":        "123",
	}

	for input, want := range cases {
		if got := stripVersionSuffix(input); got != want {
			t.Errorf("stripVersionSuffix(%q) = %q, want %q", input, got, want)
		}
	}
}
