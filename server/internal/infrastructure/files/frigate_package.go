package files

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	frigatePackageFileName   = "package.deb"
	frigatePackageStagingDir = "staging"
)

// ErrFrigatePackageMissing is returned when a package's blob is not on this
// host's disk, for example after Fleet failed over to a replica that never
// received the upload.
var ErrFrigatePackageMissing = errors.New("frigate package is not stored on this host")

// FrigatePackageStore keeps uploaded Frigate Debian packages on local disk,
// one directory per package ID. The store holds bytes only; metadata and the
// signed release statement live in the database. Directory names are canonical
// UUIDs and the file name is fixed, so no caller-supplied name reaches the
// filesystem.
type FrigatePackageStore struct {
	root string
}

// NewFrigatePackageStore creates the store directories under root and removes
// staging files abandoned by an earlier process.
func NewFrigatePackageStore(root string) (*FrigatePackageStore, error) {
	if root == "" {
		return nil, errors.New("frigate package directory is required")
	}
	staging := filepath.Join(root, frigatePackageStagingDir)
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return nil, fmt.Errorf("create frigate package directory: %w", err)
	}
	cleanStorageStagingDir(staging,
		"failed to remove orphaned frigate package staging entry",
		"removed orphaned frigate package staging entry")
	return &FrigatePackageStore{root: root}, nil
}

// NewStagingPath reserves an empty staging file for a chunked upload. Anything
// left behind is removed on the next start.
func (s *FrigatePackageStore) NewStagingPath(uploadID string) (string, error) {
	if uploadID == "" || uploadID != filepath.Base(uploadID) || uploadID == "." || uploadID == ".." {
		return "", fleeterror.NewInvalidArgumentErrorf("invalid upload ID %q", uploadID)
	}
	path := filepath.Join(s.root, frigatePackageStagingDir, uploadID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create frigate package staging file: %v", err)
	}
	if err := f.Close(); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to close frigate package staging file: %v", err)
	}
	return path, nil
}

func (s *FrigatePackageStore) dir(id uuid.UUID) string {
	return filepath.Join(s.root, id.String())
}

// Path returns the stored blob's path.
func (s *FrigatePackageStore) Path(id uuid.UUID) string {
	return filepath.Join(s.dir(id), frigatePackageFileName)
}

// Publish moves a complete staged file into the store under id. The staged
// file must be on the same filesystem, which NewStagingPath guarantees. The
// file and directory are synced so a crash cannot leave a short blob that the
// database already references.
func (s *FrigatePackageStore) Publish(id uuid.UUID, stagedPath string) error {
	dir := s.dir(id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create frigate package directory: %w", err)
	}
	staged, err := os.OpenFile(stagedPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open staged frigate package: %w", err)
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		return fmt.Errorf("sync staged frigate package: %w", err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("close staged frigate package: %w", err)
	}
	if err := os.Rename(stagedPath, s.Path(id)); err != nil {
		_ = os.Remove(dir)
		return fmt.Errorf("publish frigate package: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return err
	}
	return syncDir(s.root)
}

// Open opens the stored blob and returns its size.
func (s *FrigatePackageStore) Open(id uuid.UUID) (*os.File, int64, error) {
	f, err := os.Open(s.Path(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, ErrFrigatePackageMissing
		}
		return nil, 0, fmt.Errorf("open frigate package: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("stat frigate package: %w", err)
	}
	return f, info.Size(), nil
}

// Exists reports whether the blob is present with the expected size.
func (s *FrigatePackageStore) Exists(id uuid.UUID, size int64) bool {
	info, err := os.Stat(s.Path(id))
	return err == nil && info.Mode().IsRegular() && info.Size() == size
}

// Delete removes a package and its directory. A missing package is not an error.
func (s *FrigatePackageStore) Delete(id uuid.UUID) error {
	if err := os.RemoveAll(s.dir(id)); err != nil {
		return fmt.Errorf("delete frigate package: %w", err)
	}
	return nil
}

// StoredPackage is a package directory found on disk.
type StoredPackage struct {
	ID uuid.UUID
	// ModTime is when the directory last changed, which for a published package
	// is when its blob was published.
	ModTime time.Time
}

// List returns the package directories in the store. Entries whose names are not
// canonical UUID directories, including the staging area, are ignored.
func (s *FrigatePackageStore) List() ([]StoredPackage, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("list frigate packages: %w", err)
	}
	var out []StoredPackage
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, err := uuid.Parse(entry.Name())
		if err != nil || id.String() != entry.Name() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // Removed while listing.
			}
			return nil, fmt.Errorf("stat frigate package directory: %w", err)
		}
		out = append(out, StoredPackage{ID: id, ModTime: info.ModTime()})
	}
	return out, nil
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
