package files

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

var (
	errStorageDirNoFile        = errors.New("no file found in storage dir")
	errStorageDirMultipleFiles = errors.New("multiple files found in storage dir")
)

func canonicalizeStorageUUID(kind, value string) (string, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid %s ID: %s", kind, value)
	}
	return parsed.String(), nil
}

// findSingleFileInDir returns the path to the single non-directory entry inside
// a directory, ignoring any named sidecars and dot-prefixed entries (temp files
// from atomic sidecar writes are never payloads). It returns an error if zero
// or more than one data file exists, so callers fail fast on corrupted storage
// dirs.
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
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, ok := ignored[e.Name()]; ok {
			continue
		}
		if foundPath != "" {
			return "", fmt.Errorf("%w: %s", errStorageDirMultipleFiles, dir)
		}
		foundPath = filepath.Join(dir, e.Name())
	}
	if foundPath == "" {
		return "", fmt.Errorf("%w: %s", errStorageDirNoFile, dir)
	}
	return foundPath, nil
}

// cleanStorageStagingDir removes every entry (files and directories) left in a
// staging dir by interrupted operations from previous runs.
func cleanStorageStagingDir(dir, failureMessage, successMessage string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			slog.Warn(failureMessage, "path", path, "error", err)
		} else if successMessage != "" {
			slog.Info(successMessage, "path", path)
		}
	}
}
