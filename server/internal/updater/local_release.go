package updater

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func (m *Manager) obtainReleaseFile(ctx context.Context, version, name, remoteURL, destination string, maxBytes int64) error {
	if m.cfg.ReleaseDir == "" {
		return m.download(ctx, remoteURL, destination, maxBytes)
	}
	// A configured directory is exclusive: missing or invalid local material
	// must never fall back to another source, including the public repository.
	return copyLocalRelease(ctx, filepath.Join(m.cfg.ReleaseDir, version, name), destination, maxBytes)
}

// copyLocalRelease snapshots operator-controlled material into the protected
// updater state directory before checksum, extraction, and identity validation.
// As with updater state, sticky shared ancestors are safe, but the release
// directory and files must not be writable by any group or other user.
func copyLocalRelease(ctx context.Context, source, destination string, maxBytes int64) error {
	directory := filepath.Dir(source)
	if err := validateTrustedDirectoryChain(directory, "local release", "release directory", true, validateDaemonPathOwner); err != nil {
		return err
	}
	for _, component := range pathComponents(directory) {
		info, err := os.Lstat(component)
		if err != nil {
			return fmt.Errorf("inspect local release directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("local release directory must not contain symlinks: %q", component)
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return fmt.Errorf("open local release directory: %w", err)
	}
	defer root.Close()
	// O_NOFOLLOW and O_NONBLOCK reject symlinks and prevent a special-file
	// replacement from blocking before the descriptor can be inspected.
	file, err := root.OpenFile(filepath.Base(source), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open local release: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect local release: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("local release must be a regular file not writable by group or others: %q", source)
	}
	if err := validateDaemonPathOwner(source, info); err != nil {
		return err
	}
	if info.Size() > maxBytes {
		return fmt.Errorf("local release is larger than %d bytes", maxBytes)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("copy local release: %w", err)
	}
	snapshot, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create protected local release copy: %w", err)
	}
	defer snapshot.Close()
	written, err := io.Copy(snapshot, io.LimitReader(readerWithContext(ctx, file), maxBytes+1))
	if err != nil {
		return fmt.Errorf("copy local release: %w", err)
	}
	if written > maxBytes {
		return fmt.Errorf("local release exceeded %d bytes", maxBytes)
	}
	if err := snapshot.Sync(); err != nil {
		return fmt.Errorf("sync local release copy: %w", err)
	}
	return nil
}
