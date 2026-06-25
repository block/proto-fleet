package files

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

func canonicalizeStorageUUID(kind, value string) (string, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid %s ID: %s", kind, value)
	}
	return parsed.String(), nil
}

// findSingleFileInDir returns the path to the single non-directory entry inside
// a directory, ignoring any named sidecars. It returns an error if zero or more
// than one data file exists, so callers fail fast on corrupted storage dirs.
func findSingleFileInDir(dir string, ignoredNames ...string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("failed to read storage dir %s: %w", dir, err)
	}
	ignored := make(map[string]struct{}, len(ignoredNames))
	for _, name := range ignoredNames {
		ignored[name] = struct{}{}
	}

	var foundPath string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, ok := ignored[e.Name()]; ok {
			continue
		}
		if foundPath != "" {
			return "", fmt.Errorf("multiple files found in %s", dir)
		}
		foundPath = filepath.Join(dir, e.Name())
	}
	if foundPath == "" {
		return "", fmt.Errorf("no file found in %s", dir)
	}
	return foundPath, nil
}
