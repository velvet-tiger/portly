package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// fileVersion is the registry file format. A file with any other version is
// rejected rather than guessed at.
const fileVersion = 1

// lockTimeout bounds how long a command waits for another portly to finish
// with the registry. A wedged process must not hang every later command.
const lockTimeout = 5 * time.Second

// Store loads and saves the registry.
type Store interface {
	// Read returns the current registry.
	Read() (Registry, error)
	// Update applies change to the current registry and saves the result. No
	// other Update can run between the load and the save. A change that
	// returns an error leaves the stored registry untouched.
	Update(change func(Registry) (Registry, error)) error
}

// DefaultPath returns where the registry file lives.
//
// stateHome is the value of XDG_STATE_HOME. The XDG spec says a relative value
// is invalid and must be ignored, so only an absolute one is used. Otherwise
// the file goes under home/.local/state, on macOS as well as Linux.
func DefaultPath(stateHome, home string) (string, error) {
	if filepath.IsAbs(stateHome) {
		return filepath.Join(stateHome, "portly", "ports.json"), nil
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("cannot place the port registry: home directory %q is not absolute", home)
	}
	return filepath.Join(home, ".local", "state", "portly", "ports.json"), nil
}

// FileStore keeps the registry in a JSON file.
//
// Writers hold an exclusive lock on a sibling lock file for the whole load,
// change and save cycle. The file is replaced by renaming a fully written temp
// file over it, so a crash never leaves a partial registry behind.
type FileStore struct {
	path string
}

// NewFileStore returns a store backed by the file at path.
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// Read returns the registry, or an empty one when the file does not exist yet.
func (s *FileStore) Read() (Registry, error) {
	if _, err := os.Stat(s.path); errors.Is(err, fs.ErrNotExist) {
		return Registry{}, nil
	}

	unlock, err := lockFile(s.lockPath(), false, lockTimeout)
	if err != nil {
		return Registry{}, err
	}
	defer unlock()

	return s.load()
}

// Update applies change under an exclusive lock and saves the result.
func (s *FileStore) Update(change func(Registry) (Registry, error)) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating the port registry directory: %w", err)
	}

	unlock, err := lockFile(s.lockPath(), true, lockTimeout)
	if err != nil {
		return err
	}
	defer unlock()

	current, err := s.load()
	if err != nil {
		return err
	}
	next, err := change(current)
	if err != nil {
		return err
	}
	return s.save(next)
}

// lockPath is the sibling file the lock is held on. Locking the registry file
// itself would not survive the rename that replaces it.
func (s *FileStore) lockPath() string {
	return s.path + ".lock"
}

// load reads and validates the registry file. A missing file is an empty
// registry. A malformed one is an error, never a silent reset, because a reset
// would hand claimed ports to other apps.
func (s *FileStore) load() (Registry, error) {
	content, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return Registry{}, nil
	}
	if err != nil {
		return Registry{}, fmt.Errorf("reading the port registry: %w", err)
	}

	registry, err := decodeRegistry(content)
	if err != nil {
		return Registry{}, fmt.Errorf("port registry %s is invalid: %w", s.path, err)
	}
	return registry, nil
}

// save writes the registry to a temp file and renames it into place.
func (s *FileStore) save(registry Registry) error {
	content, err := encodeRegistry(registry)
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(s.path), ".ports-*.json")
	if err != nil {
		return fmt.Errorf("creating a temp file for the port registry: %w", err)
	}
	tempPath := temp.Name()

	if err := writeAndClose(temp, content); err != nil {
		// The temp file is incomplete and never became the registry. Removing
		// it is cleanup only, so its own failure is not worth reporting over
		// the write error.
		_ = os.Remove(tempPath)
		return fmt.Errorf("writing the port registry: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replacing the port registry: %w", err)
	}
	return nil
}

// writeAndClose writes content, flushes it to disk, and closes the file.
func writeAndClose(file *os.File, content []byte) error {
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// fileDocument is the registry's on-disk shape. It is declared apart from the
// internal types so renaming a field cannot silently change the file format.
type fileDocument struct {
	Version int         `json:"version"`
	Claims  []fileClaim `json:"claims"`
}

type fileClaim struct {
	Port         uint32    `json:"port"`
	Project      string    `json:"project"`
	ProjectDir   string    `json:"project_dir"`
	Name         string    `json:"name"`
	RegisteredAt time.Time `json:"registered_at"`
	// PID is null for a claim not tied to a process.
	PID *int32 `json:"pid"`
}

// encodeRegistry renders the registry in its file format.
func encodeRegistry(registry Registry) ([]byte, error) {
	document := fileDocument{Version: fileVersion, Claims: make([]fileClaim, 0, len(registry.Claims))}
	for _, claim := range registry.Claims {
		converted := fileClaim{
			Port:         claim.Port,
			Project:      claim.Project,
			ProjectDir:   claim.ProjectDir,
			Name:         claim.Name,
			RegisteredAt: claim.RegisteredAt,
		}
		if claim.PID != 0 {
			pid := claim.PID
			converted.PID = &pid
		}
		document.Claims = append(document.Claims, converted)
	}

	content, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding the port registry: %w", err)
	}
	return append(content, '\n'), nil
}

// decodeRegistry parses and validates the registry file format.
func decodeRegistry(content []byte) (Registry, error) {
	var document fileDocument
	if err := json.Unmarshal(content, &document); err != nil {
		return Registry{}, err
	}
	if document.Version != fileVersion {
		return Registry{}, fmt.Errorf("version %d is not supported, want %d", document.Version, fileVersion)
	}

	claims := make([]Claim, 0, len(document.Claims))
	ports := make(map[uint32]bool, len(document.Claims))
	identities := make(map[[2]string]bool, len(document.Claims))

	for index, stored := range document.Claims {
		claim := Claim{
			Port:         stored.Port,
			ProjectDir:   stored.ProjectDir,
			Project:      stored.Project,
			Name:         stored.Name,
			RegisteredAt: stored.RegisteredAt,
		}
		if stored.PID != nil {
			claim.PID = *stored.PID
		}
		if err := validateStoredClaim(claim); err != nil {
			return Registry{}, fmt.Errorf("claim %d: %w", index, err)
		}

		identity := [2]string{claim.ProjectDir, claim.Name}
		if ports[claim.Port] {
			return Registry{}, fmt.Errorf("claim %d: port %d is claimed twice", index, claim.Port)
		}
		if identities[identity] {
			return Registry{}, fmt.Errorf("claim %d: %s holds two claims", index, claim.Describe())
		}
		ports[claim.Port] = true
		identities[identity] = true
		claims = append(claims, claim)
	}
	return Registry{Claims: sortedByPort(claims)}, nil
}

// validateStoredClaim rejects a claim the registry could never have written.
func validateStoredClaim(claim Claim) error {
	switch {
	case claim.Port == 0 || claim.Port > 65535:
		return fmt.Errorf("port %d is out of range", claim.Port)
	case claim.Name == "":
		return fmt.Errorf("name is empty")
	case claim.Project == "":
		return fmt.Errorf("project is empty")
	case !filepath.IsAbs(claim.ProjectDir):
		return fmt.Errorf("project_dir %q is not absolute", claim.ProjectDir)
	case claim.PID < 0:
		return fmt.Errorf("pid %d is negative", claim.PID)
	}
	return nil
}
