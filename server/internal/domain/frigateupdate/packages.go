package frigateupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/frigaterelease"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const (
	packageUploadedEventType = "frigate_package_uploaded"
	packageDeletedEventType  = "frigate_package_deleted"
	maxFilenameLength        = 255
	// orphanBlobGrace is how long a blob directory without a package row is left
	// alone. A blob is published before its row is inserted, so a younger
	// directory may belong to an import that is still in progress.
	orphanBlobGrace = time.Hour
)

// Package is an uploaded, signature-checked Frigate package.
type Package struct {
	ID                  uuid.UUID
	Filename            string
	Version             string
	Architecture        string
	SHA256              string
	SizeBytes           int64
	Repository          string
	ReleaseKeyID        string
	MinInstalledVersion string
	IssuedAt            time.Time
	UploadedAt          time.Time
	UploadedBy          string
	Deleted             bool
	// StoredOnThisHost reports whether the package bytes are on the disk of the
	// Fleet replica that answered. Only ListPackages fills it.
	StoredOnThisHost bool
}

func packageFromRow(r sqlc.GetFrigateUpdatePackageRow) Package {
	return Package{
		ID: r.ID, Filename: r.Filename, Version: r.Version, Architecture: r.Architecture, SHA256: r.Sha256,
		SizeBytes: r.SizeBytes, Repository: r.Repository, ReleaseKeyID: r.ReleaseKeyID,
		MinInstalledVersion: r.MinInstalledVersion, IssuedAt: r.IssuedAt, UploadedAt: r.UploadedAt,
		UploadedBy: r.UploadedByUsername, Deleted: r.DeletedAt.Valid,
	}
}

func packageFromListRow(r sqlc.ListFrigateUpdatePackagesRow) Package {
	return Package{
		ID: r.ID, Filename: r.Filename, Version: r.Version, Architecture: r.Architecture, SHA256: r.Sha256,
		SizeBytes: r.SizeBytes, Repository: r.Repository, ReleaseKeyID: r.ReleaseKeyID,
		MinInstalledVersion: r.MinInstalledVersion, IssuedAt: r.IssuedAt, UploadedAt: r.UploadedAt,
		UploadedBy: r.UploadedByUsername,
	}
}

func packageFromModel(r sqlc.FrigateUpdatePackage, uploader string) Package {
	return Package{
		ID: r.ID, Filename: r.Filename, Version: r.Version, Architecture: r.Architecture, SHA256: r.Sha256,
		SizeBytes: r.SizeBytes, Repository: r.Repository, ReleaseKeyID: r.ReleaseKeyID,
		MinInstalledVersion: r.MinInstalledVersion, IssuedAt: r.IssuedAt, UploadedAt: r.UploadedAt,
		UploadedBy: uploader, Deleted: r.DeletedAt.Valid,
	}
}

// ValidateUploadStatement checks a release statement and its signature before
// any package bytes are accepted, so an operator learns about a bad signature or
// an oversized package immediately. Fleet's check is advisory: every controller
// re-verifies the signature against its own trusted keys.
func (s *Service) ValidateUploadStatement(ctx context.Context, statement, signature []byte, filename string, size int64) (frigaterelease.Verified, error) {
	if err := s.auth.AuthorizeAnySite(ctx); err != nil {
		return frigaterelease.Verified{}, err
	}
	if _, err := principal(ctx); err != nil {
		return frigaterelease.Verified{}, err
	}
	if err := validateFilename(filename); err != nil {
		return frigaterelease.Verified{}, err
	}
	verified, err := s.verify(statement, signature)
	if err != nil {
		return frigaterelease.Verified{}, err
	}
	if size <= 0 {
		return frigaterelease.Verified{}, fleeterror.NewInvalidArgumentError("package size must be greater than zero")
	}
	if size != verified.Statement.Size {
		return frigaterelease.Verified{}, fleeterror.NewInvalidArgumentErrorf("package size %d does not match the signed release statement (%d)", size, verified.Statement.Size)
	}
	if size > s.cfg.MaxPackageSize {
		return frigaterelease.Verified{}, fleeterror.NewInvalidArgumentErrorf("package size exceeds the maximum allowed size (%d bytes)", s.cfg.MaxPackageSize)
	}
	return verified, nil
}

func validateFilename(name string) error {
	if name == "" || len(name) > maxFilenameLength {
		return fleeterror.NewInvalidArgumentError("filename is required and must be at most 255 bytes")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return fleeterror.NewInvalidArgumentError("filename contains invalid characters")
		}
	}
	return nil
}

func (s *Service) verify(statement, signature []byte) (frigaterelease.Verified, error) {
	if s.verifier == nil {
		return frigaterelease.Verified{}, fleeterror.NewFailedPreconditionError("Controller software upload is disabled: no release keys are configured")
	}
	verified, err := s.verifier.Verify(statement, signature, "")
	switch {
	case err == nil:
		return verified, nil
	case errors.Is(err, frigaterelease.ErrNoTrustedKeys):
		return frigaterelease.Verified{}, fleeterror.NewFailedPreconditionError("Controller software upload is disabled: no release keys are configured")
	case errors.Is(err, frigaterelease.ErrSignatureInvalid),
		errors.Is(err, frigaterelease.ErrKeyIDMismatch),
		errors.Is(err, frigaterelease.ErrStatementInvalid):
		return frigaterelease.Verified{}, fleeterror.NewInvalidArgumentErrorf("release statement rejected: %v", err)
	default:
		slog.Error("frigate release verification failed", "error", err)
		return frigaterelease.Verified{}, fleeterror.NewInternalError("failed to verify the release statement")
	}
}

// ImportInput describes a fully uploaded package waiting in the staging area.
type ImportInput struct {
	// StagedPath is the staged file created by FrigatePackageStore.NewStagingPath.
	// ImportPackage consumes it: on return it is either published or removed.
	StagedPath string
	Filename   string
	Statement  []byte
	Signature  []byte
}

// ImportResult is the outcome of ImportPackage.
type ImportResult struct {
	Package Package
	// Reused is true when an identical package was already stored.
	Reused bool
}

// ImportPackage verifies an uploaded package against its signed release
// statement and stores it. It checks the signature, the file size and SHA-256
// against the statement, and the Debian control data against the statement.
func (s *Service) ImportPackage(ctx context.Context, in ImportInput) (ImportResult, error) {
	defer func() { _ = os.Remove(in.StagedPath) }() // A no-op once the file was published.
	if err := s.auth.AuthorizeAnySite(ctx); err != nil {
		return ImportResult{}, err
	}
	info, err := principal(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	if err := validateFilename(in.Filename); err != nil {
		return ImportResult{}, err
	}
	verified, err := s.verify(in.Statement, in.Signature)
	if err != nil {
		return ImportResult{}, err
	}
	statement := verified.Statement
	if statement.Size > s.cfg.MaxPackageSize {
		return ImportResult{}, fleeterror.NewInvalidArgumentErrorf("package size exceeds the maximum allowed size (%d bytes)", s.cfg.MaxPackageSize)
	}

	stat, err := os.Stat(in.StagedPath)
	if err != nil {
		return ImportResult{}, fleeterror.NewInternalErrorf("failed to read the uploaded package: %v", err)
	}
	if stat.Size() != statement.Size {
		return ImportResult{}, fleeterror.NewInvalidArgumentErrorf("package size %d does not match the signed release statement (%d)", stat.Size(), statement.Size)
	}
	digest, err := hashFile(in.StagedPath)
	if err != nil {
		return ImportResult{}, fleeterror.NewInternalErrorf("failed to hash the uploaded package: %v", err)
	}
	if digest != statement.SHA256 {
		return ImportResult{}, fleeterror.NewInvalidArgumentError("package SHA-256 does not match the signed release statement")
	}
	control, err := frigaterelease.ReadDebControl(in.StagedPath)
	if err != nil {
		return ImportResult{}, controlError(err)
	}
	if err := control.Matches(statement); err != nil {
		return ImportResult{}, fleeterror.NewInvalidArgumentErrorf("package does not match the signed release statement: %v", err)
	}

	existing, err := s.queries.GetFrigateUpdatePackageBySHA256(ctx, sqlc.GetFrigateUpdatePackageBySHA256Params{OrgID: info.OrganizationID, Sha256: digest})
	switch {
	case err == nil:
		existing, err = s.refreshRelease(ctx, existing, verified, in)
		if err != nil {
			return ImportResult{}, err
		}
		if !s.store.Exists(existing.ID, existing.SizeBytes) {
			// The row survived but the blob did not (restored database, new
			// host). Republish the verified bytes under the existing ID.
			if err := s.store.Publish(existing.ID, in.StagedPath); err != nil {
				return ImportResult{}, fleeterror.NewInternalErrorf("failed to store the package: %v", err)
			}
		}
		return ImportResult{Package: packageFromModel(existing, ""), Reused: true}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return ImportResult{}, err
	}

	id := uuid.New()
	if err := s.store.Publish(id, in.StagedPath); err != nil {
		return ImportResult{}, fleeterror.NewInternalErrorf("failed to store the package: %v", err)
	}
	row, err := s.queries.InsertFrigateUpdatePackage(ctx, sqlc.InsertFrigateUpdatePackageParams{
		ID: id, OrgID: info.OrganizationID, Filename: in.Filename, Version: statement.Version,
		Architecture: statement.Architecture, Sha256: statement.SHA256, SizeBytes: statement.Size,
		Repository: statement.Repository, ReleaseKeyID: verified.KeyID, MinInstalledVersion: statement.MinInstalledVersion,
		IssuedAt: statement.IssuedAt, Statement: in.Statement, Signature: in.Signature,
		UploadedBy: sql.NullInt64{Int64: info.UserID, Valid: true},
	})
	if err != nil {
		_ = s.store.Delete(id)
		if db.IsUniqueViolationError(err) {
			// A concurrent upload of the same package won the race.
			winner, getErr := s.queries.GetFrigateUpdatePackageBySHA256(ctx, sqlc.GetFrigateUpdatePackageBySHA256Params{OrgID: info.OrganizationID, Sha256: digest})
			if getErr == nil {
				return ImportResult{Package: packageFromModel(winner, ""), Reused: true}, nil
			}
		}
		return ImportResult{}, fleeterror.NewInternalErrorf("failed to record the package: %v", err)
	}

	s.logPackageEvent(ctx, info.OrganizationID, packageUploadedEventType,
		fmt.Sprintf("Uploaded controller software %s (%s)", statement.Version, statement.Architecture), row, info.ExternalUserID, info.Username)
	return ImportResult{Package: packageFromModel(row, info.Username)}, nil
}

// refreshRelease applies a freshly verified release statement to a package that
// is already stored. The bytes are identical, so only the statement-derived
// fields can differ, for example after a key rotation re-signed the release.
// A re-upload of the same statement and key keeps the stored signature: ECDSA
// signatures differ on every signing, and both verify. A package that a
// blocking job references keeps its statement, because the controllers hold the
// manifest Fleet built from it; replacing it then is refused.
func (s *Service) refreshRelease(ctx context.Context, existing sqlc.FrigateUpdatePackage, verified frigaterelease.Verified, in ImportInput) (sqlc.FrigateUpdatePackage, error) {
	if bytes.Equal(existing.Statement, in.Statement) && existing.ReleaseKeyID == verified.KeyID {
		return existing, nil
	}
	statement := verified.Statement
	updated, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.FrigateUpdatePackage, error) {
		locked, err := q.LockFrigateUpdatePackage(ctx, sqlc.LockFrigateUpdatePackageParams{OrgID: existing.OrgID, ID: existing.ID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return sqlc.FrigateUpdatePackage{}, fleeterror.NewFailedPreconditionError("the package was deleted while it was being uploaded; upload it again")
			}
			return sqlc.FrigateUpdatePackage{}, err
		}
		inUse, err := q.FrigateUpdatePackageInUse(ctx, locked.ID)
		if err != nil {
			return sqlc.FrigateUpdatePackage{}, err
		}
		if inUse {
			return sqlc.FrigateUpdatePackage{}, fleeterror.NewFailedPreconditionError(
				"this package is already stored with a different release statement or signing key, and an update that is still in progress uses it; finish or cancel that update, then upload the package again")
		}
		if err := q.UpdateFrigateUpdatePackageRelease(ctx, sqlc.UpdateFrigateUpdatePackageReleaseParams{
			OrgID: locked.OrgID, ID: locked.ID, Version: statement.Version, Architecture: statement.Architecture,
			Repository: statement.Repository, ReleaseKeyID: verified.KeyID, MinInstalledVersion: statement.MinInstalledVersion,
			IssuedAt: statement.IssuedAt, Statement: in.Statement, Signature: in.Signature,
		}); err != nil {
			return sqlc.FrigateUpdatePackage{}, err
		}
		return q.GetFrigateUpdatePackageBySHA256(ctx, sqlc.GetFrigateUpdatePackageBySHA256Params{OrgID: locked.OrgID, Sha256: locked.Sha256})
	})
	if err != nil {
		var fe fleeterror.FleetError
		if errors.As(err, &fe) {
			return sqlc.FrigateUpdatePackage{}, err
		}
		return sqlc.FrigateUpdatePackage{}, fleeterror.NewInternalErrorf("failed to update the package release statement: %v", err)
	}
	return updated, nil
}

func controlError(err error) error {
	switch {
	case errors.Is(err, frigaterelease.ErrUnsupportedCompression):
		return fleeterror.NewInvalidArgumentErrorf("the package cannot be checked by Fleet: %v. Rebuild it with a gzip-compressed control archive", err)
	case errors.Is(err, frigaterelease.ErrPackageInvalid):
		return fleeterror.NewInvalidArgumentErrorf("the uploaded file is not a valid controller software package: %v", err)
	default:
		return fleeterror.NewInternalErrorf("failed to read the package: %v", err)
	}
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open package: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash package: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Service) logPackageEvent(ctx context.Context, orgID int64, eventType, description string, pkg sqlc.FrigateUpdatePackage, externalID, username string) {
	event := activitymodels.Event{
		Category:       activitymodels.CategorySystem,
		Type:           eventType,
		Description:    description,
		ActorType:      activitymodels.ActorUser,
		UserID:         &externalID,
		Username:       &username,
		OrganizationID: &orgID,
		Metadata: map[string]any{
			"package_id":   pkg.ID.String(),
			"version":      pkg.Version,
			"architecture": pkg.Architecture,
			"sha256":       pkg.Sha256,
			"filename":     pkg.Filename,
		},
		IdempotencyKey: fmt.Sprintf("%s:%s", eventType, pkg.ID),
	}
	s.activity.Log(ctx, event)
}

// ListPackages returns the organization's available packages, newest first.
func (s *Service) ListPackages(ctx context.Context) ([]Package, error) {
	if err := s.auth.AuthorizeAnySite(ctx); err != nil {
		return nil, err
	}
	info, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListFrigateUpdatePackages(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	out := make([]Package, 0, len(rows))
	for _, r := range rows {
		p := packageFromListRow(r)
		p.StoredOnThisHost = s.store.Exists(r.ID, r.SizeBytes)
		out = append(out, p)
	}
	return out, nil
}

// HasUpdatableContainer reports whether a container is paired at a site where
// the caller may update controller software. Package management is offered to
// callers by this answer rather than by which containers they can list, because
// reading inventory and updating controller software are separate permissions.
func (s *Service) HasUpdatableContainer(ctx context.Context) (bool, error) {
	if err := s.auth.AuthorizeAnySite(ctx); err != nil {
		return false, err
	}
	info, err := principal(ctx)
	if err != nil {
		return false, err
	}
	containers, err := s.queries.ListContainerPlacements(ctx, info.OrganizationID)
	if err != nil {
		return false, err
	}
	for _, c := range containers {
		if s.auth.AuthorizeSite(ctx, siteArg(c.SiteID)) == nil {
			return true, nil
		}
	}
	return false, nil
}

// DeletePackage removes an uploaded package that no in-progress update uses.
func (s *Service) DeletePackage(ctx context.Context, id uuid.UUID) error {
	if err := s.auth.AuthorizeOrgWide(ctx); err != nil {
		return err
	}
	info, err := principal(ctx)
	if err != nil {
		return err
	}
	deleted, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.FrigateUpdatePackage, error) {
		pkg, err := q.LockFrigateUpdatePackage(ctx, sqlc.LockFrigateUpdatePackageParams{OrgID: info.OrganizationID, ID: id})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return sqlc.FrigateUpdatePackage{}, fleeterror.NewNotFoundError("package not found")
			}
			return sqlc.FrigateUpdatePackage{}, err
		}
		inUse, err := q.FrigateUpdatePackageInUse(ctx, id)
		if err != nil {
			return sqlc.FrigateUpdatePackage{}, err
		}
		if inUse {
			return sqlc.FrigateUpdatePackage{}, fleeterror.NewFailedPreconditionError("the package is used by an update that is still in progress")
		}
		if err := q.SoftDeleteFrigateUpdatePackage(ctx, sqlc.SoftDeleteFrigateUpdatePackageParams{OrgID: info.OrganizationID, ID: id}); err != nil {
			return sqlc.FrigateUpdatePackage{}, err
		}
		return pkg, nil
	})
	if err != nil {
		return err
	}
	if err := s.store.Delete(id); err != nil {
		// The row is gone; the orphaned blob is harmless and logged for cleanup.
		slog.Warn("failed to delete frigate package blob", "package_id", id, "error", err)
	}
	s.logPackageEvent(ctx, info.OrganizationID, packageDeletedEventType,
		fmt.Sprintf("Deleted controller software %s (%s)", deleted.Version, deleted.Architecture), deleted, info.ExternalUserID, info.Username)
	return nil
}

// SweepOrphanPackages removes blob directories that no live package row owns:
// one left by a crash between publishing the blob and recording the package, or
// one whose delete failed after the row was soft-deleted. It returns how many it
// removed. Directories younger than the grace period are skipped.
func (s *Service) SweepOrphanPackages(ctx context.Context) (int, error) {
	stored, err := s.store.List()
	if err != nil {
		return 0, fmt.Errorf("list stored frigate packages: %w", err)
	}
	removed := 0
	for _, entry := range stored {
		if err := ctx.Err(); err != nil {
			return removed, fmt.Errorf("sweep orphan frigate packages: %w", err)
		}
		if s.now().Sub(entry.ModTime) < orphanBlobGrace {
			continue
		}
		wanted, err := s.queries.FrigateUpdatePackageWantsBlob(ctx, entry.ID)
		if err != nil {
			return removed, fmt.Errorf("check frigate package %s: %w", entry.ID, err)
		}
		if wanted {
			continue
		}
		if err := s.store.Delete(entry.ID); err != nil {
			slog.Warn("failed to remove orphaned frigate package blob", "package_id", entry.ID, "error", err)
			continue
		}
		slog.Info("removed orphaned frigate package blob", "package_id", entry.ID)
		removed++
	}
	return removed, nil
}
