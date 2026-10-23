package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPath(t *testing.T) {
	cases := []struct {
		name      string
		stateHome string
		home      string
		want      string
		wantErr   bool
	}{
		{name: "XDG_STATE_HOME wins", stateHome: "/state", home: "/Users/dev", want: "/state/portly/ports.json"},
		{name: "unset XDG_STATE_HOME falls back to home", home: "/Users/dev", want: "/Users/dev/.local/state/portly/ports.json"},
		{name: "relative XDG_STATE_HOME is ignored per the spec", stateHome: "state", home: "/Users/dev", want: "/Users/dev/.local/state/portly/ports.json"},
		{name: "no usable location is an error", home: "", wantErr: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := DefaultPath(testCase.stateHome, testCase.home)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("DefaultPath() returned %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DefaultPath() returned %v", err)
			}
			if got != testCase.want {
				t.Errorf("DefaultPath() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestFileStoreReadsAMissingFileAsEmpty(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "nested", "ports.json"))

	registry, err := store.Read()
	if err != nil {
		t.Fatalf("Read() returned %v", err)
	}
	if len(registry.Claims) != 0 {
		t.Errorf("got %d claims, want 0", len(registry.Claims))
	}
}

func TestFileStoreRoundTripsClaims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "portly", "ports.json")
	store := NewFileStore(path)
	tied := apiClaim()
	tied.PID = 4242

	err := store.Update(func(current Registry) (Registry, error) {
		return current.With(webClaim()).With(tied), nil
	})
	if err != nil {
		t.Fatalf("Update() returned %v", err)
	}

	registry, err := store.Read()
	if err != nil {
		t.Fatalf("Read() returned %v", err)
	}
	if len(registry.Claims) != 2 {
		t.Fatalf("got %d claims, want 2", len(registry.Claims))
	}
	if registry.Claims[0] != webClaim() {
		t.Errorf("first claim = %+v, want %+v", registry.Claims[0], webClaim())
	}
	if registry.Claims[1] != tied {
		t.Errorf("second claim = %+v, want %+v", registry.Claims[1], tied)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the registry file: %v", err)
	}
	if !strings.Contains(string(content), `"pid": null`) || !strings.Contains(string(content), `"pid": 4242`) {
		t.Errorf("file does not record PIDs as designed:\n%s", content)
	}
}

func TestFileStoreLeavesTheFileUntouchedWhenTheChangeFails(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "ports.json"))
	if err := store.Update(func(current Registry) (Registry, error) { return current.With(webClaim()), nil }); err != nil {
		t.Fatalf("seeding Update() returned %v", err)
	}

	refusal := errors.New("refused")
	err := store.Update(func(current Registry) (Registry, error) {
		return current.With(apiClaim()), refusal
	})
	if !errors.Is(err, refusal) {
		t.Fatalf("Update() returned %v, want the change's error", err)
	}

	registry, err := store.Read()
	if err != nil {
		t.Fatalf("Read() returned %v", err)
	}
	if len(registry.Claims) != 1 {
		t.Errorf("got %d claims, want the original 1", len(registry.Claims))
	}
}

func TestFileStoreLeavesNoTempFilesBehind(t *testing.T) {
	directory := t.TempDir()
	store := NewFileStore(filepath.Join(directory, "ports.json"))
	if err := store.Update(func(current Registry) (Registry, error) { return current.With(webClaim()), nil }); err != nil {
		t.Fatalf("Update() returned %v", err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("listing the directory: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "ports.json" && entry.Name() != "ports.json.lock" {
			t.Errorf("unexpected file %q left behind", entry.Name())
		}
	}
}

func TestFileStoreRejectsAMalformedFile(t *testing.T) {
	cases := map[string]string{
		"not JSON":          `{`,
		"unknown version":   `{"version": 2, "claims": []}`,
		"port zero":         `{"version": 1, "claims": [{"port": 0, "project": "a", "project_dir": "/a", "name": "web"}]}`,
		"relative dir":      `{"version": 1, "claims": [{"port": 4000, "project": "a", "project_dir": "a", "name": "web"}]}`,
		"empty name":        `{"version": 1, "claims": [{"port": 4000, "project": "a", "project_dir": "/a", "name": ""}]}`,
		"duplicate port":    `{"version": 1, "claims": [{"port": 4000, "project": "a", "project_dir": "/a", "name": "web"}, {"port": 4000, "project": "b", "project_dir": "/b", "name": "web"}]}`,
		"duplicate claimer": `{"version": 1, "claims": [{"port": 4000, "project": "a", "project_dir": "/a", "name": "web"}, {"port": 4001, "project": "a", "project_dir": "/a", "name": "web"}]}`,
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ports.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("writing the fixture: %v", err)
			}

			if _, err := NewFileStore(path).Read(); err == nil {
				t.Fatal("Read() returned nil error, want one")
			}
			err := NewFileStore(path).Update(func(current Registry) (Registry, error) { return current, nil })
			if err == nil {
				t.Fatal("Update() returned nil error, want one")
			}

			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("reading the fixture back: %v", readErr)
			}
			if string(after) != content {
				t.Error("a malformed registry was overwritten")
			}
		})
	}
}
