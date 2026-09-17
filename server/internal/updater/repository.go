package updater

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/block/proto-fleet/server/internal/releaseinfo"
)

const repositoryFilename = "release-repository"

// pinReleaseRepository runs with the updater process lock held. The pin lives
// in the protected daemon state directory, outside the application deployment
// and its swap/rollback lifecycle. Neither API input nor application-owned
// configuration can change this trust anchor.
func pinReleaseRepository(cfg Config, repository string) error {
	path := filepath.Join(cfg.StateDir, repositoryFilename)
	info, err := os.Lstat(path)
	pinned := err == nil
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("updater release repository pin must be a protected regular file")
		}
		if err := validateTrustedUpdaterOwner(path, info); err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read updater release repository pin: %w", err)
		}
		pinnedRepository := strings.TrimSuffix(string(contents), "\n")
		if releaseinfo.ValidateRepository(pinnedRepository) != nil || pinnedRepository != repository {
			return fmt.Errorf("embedded release repository conflicts with the updater's persisted source; cross-repository migration is not supported")
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect updater release repository pin: %w", err)
	}
	// A crash can leave the current tree at deployment.previous. Both trees
	// must agree before accepting the source or repairing an interrupted swap.
	for _, name := range []string{"deployment", "deployment.previous"} {
		metadataPath := filepath.Join(cfg.InstallRoot, name, "version.txt")
		if _, err := os.Stat(metadataPath); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := checkDeploymentRepository(filepath.Join(cfg.InstallRoot, name), repository); err != nil {
			return err
		}
	}
	if pinned {
		return nil
	}
	file, err := os.CreateTemp(cfg.StateDir, ".release-repository-")
	if err != nil {
		return fmt.Errorf("create release repository pin: %w", err)
	}
	defer os.Remove(file.Name())
	_, writeErr := file.WriteString(repository + "\n")
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return fmt.Errorf("persist release repository pin: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("activate release repository pin: %w", err)
	}
	return syncDirectory(cfg.StateDir)
}

func checkDeploymentRepository(deployment, expected string) error {
	contents, err := os.ReadFile(filepath.Join(deployment, "version.txt"))
	if err != nil {
		return fmt.Errorf("read deployment release repository: %w", err)
	}
	repository, err := releaseinfo.RepositoryFromMetadata(contents)
	if err != nil {
		return err
	}
	if repository != expected {
		return fmt.Errorf("deployment release repository %s conflicts with trusted updater source %s", repository, expected)
	}
	return nil
}
