package classify

import (
	"os"
	"path/filepath"
)

// maxProjectWalk bounds the upward search for a project root. Deeply nested
// working directories are rare, and a bound keeps the cost at a handful of
// stat calls per listener.
const maxProjectWalk = 12

// projectMarkers are files and directories that identify a project root.
//
// Matching on markers rather than on a list of parent directories keeps the
// rule independent of where any particular machine keeps its code.
var projectMarkers = []string{
	".git",
	"package.json",
	"composer.json",
	"pyproject.toml",
	"go.mod",
	"Cargo.toml",
	"Gemfile",
	"pom.xml",
	"build.gradle",
	"deno.json",
	"requirements.txt",
	"docker-compose.yml",
	"docker-compose.yaml",
}

// Project is a directory identified as the root of a codebase.
type Project struct {
	// Root is the absolute path of the directory holding the marker.
	Root string
	// Name is the directory's base name, used for display.
	Name string
	// Marker is the file or directory that identified the root, which explains
	// the attribution to anyone who disagrees with it.
	Marker string
}

// DirectoryReader reports whether a path exists.
//
// It exists so project detection can be tested against a fabricated tree
// instead of the real filesystem.
type DirectoryReader interface {
	Exists(path string) bool
}

// OSDirectoryReader checks the real filesystem.
type OSDirectoryReader struct{}

// Exists reports whether path is present.
func (OSDirectoryReader) Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// FindProject walks up from directory looking for a project marker.
//
// It returns the nearest match, so a monorepo package resolves to the package
// rather than to the repository root. It returns false when the directory is
// empty, is not absolute, or when no marker is found within the walk bound.
func FindProject(reader DirectoryReader, directory string) (Project, bool) {
	if directory == "" || !filepath.IsAbs(directory) {
		return Project{}, false
	}

	current := filepath.Clean(directory)

	for depth := 0; depth < maxProjectWalk; depth++ {
		for _, marker := range projectMarkers {
			if reader.Exists(filepath.Join(current, marker)) {
				return Project{
					Root:   current,
					Name:   filepath.Base(current),
					Marker: marker,
				}, true
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			return Project{}, false
		}
		current = parent
	}
	return Project{}, false
}
