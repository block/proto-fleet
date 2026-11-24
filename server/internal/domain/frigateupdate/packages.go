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

// MaxReleasePublicKeyBytes bounds a public key supplied with an upload. One
// PEM-encoded ECDSA P-256 public key is about 180 bytes. The limit keeps the
// largest statement, signature and key inside the upload handler's body limit.
const MaxReleasePublicKeyBytes = 4 << 10

// SignatureCheck says how Fleet checked a release signature at upload.
type SignatureCheck string

const (
	// SignatureInstalledKey means the signature verified against a release key
	// installed on Fleet.
	SignatureInstalledKey SignatureCheck = "installed_key"
	// SignatureUploadedKey means Fleet has no installed release key and the
	// signature verified against the public key supplied with the upload.
	SignatureUploadedKey SignatureCheck = "uploaded_key"
	// SignatureUnverified means Fleet has no installed release key and did not
	// verify the signature: no public key was supplied, or the signature does
	// not verify with the supplied one.
	SignatureUnverified SignatureCheck = "unverified"
)

// Operator-facing text. It is short on purpose: one sentence for what happened
// and one for what happens next.
const (
	warnNoReleaseKey       = "Fleet has no release key to verify the signature. The controllers verify it before they install."
	warnReleaseKeyMismatch = "The signature does not match the public key. The controllers verify it before they install."

	errSignatureFile  = "The release signature file is not a valid signature."
	errPublicKeyFile  = "The public key file is not a valid ECDSA P-256 public key."
	errVerifiedStored = "This package is already stored and verified. To replace its release statement, " +
		"upload it with the public key, or delete the stored package first."
)

// ReleaseCheck is the outcome of checking a release statement at upload.
type ReleaseCheck struct {
	Statement frigaterelease.Statement
	// KeyID identifies the key that verified the signature. It is empty when
	// Fleet did not verify the signature.
	KeyID     string
	Signature SignatureCheck
	// Warning is operator-facing text. It is set only when Signature is
	// SignatureUnverified.
	Warning string
}

// Package is an uploaded Frigate package. Fleet checked it against its release
// statement, and checked the statement's signature when it had a key to check
// it with.
type Package struct {
	ID           uuid.UUID
	Filename     string
	Version      string
	Architecture string
	SHA256       string
	SizeBytes    int64
	Repository   string
	// ReleaseKeyID identifies the key that verified the release signature. It
	// is empty when Fleet stored the package without verifying the signature.
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
// any package bytes are accepted, so an operator learns about a bad statement or
// an oversized package immediately. publicKey is the optional PEM public key
// supplied with the upload; see checkRelease for when it is used. Fleet's check
// is advisory: every controller re-verifies the signature against its own
// trusted keys.
func (s *Service) ValidateUploadStatement(ctx context.Context, statement, signature, publicKey []byte, filename string, size int64) (ReleaseCheck, error) {
	if err := s.auth.AuthorizeAnySite(ctx); err != nil {
		return ReleaseCheck{}, err
	}
	if _, err := principal(ctx); err != nil {
		return ReleaseCheck{}, err
	}
	if err := validateFilename(filename); err != nil {
		return ReleaseCheck{}, err
	}
	check, err := s.checkRelease(statement, signature, publicKey)
	if err != nil {
		return ReleaseCheck{}, err
	}
	if size <= 0 {
		return ReleaseCheck{}, fleeterror.NewInvalidArgumentError("package size must be greater than zero")
	}
	if size != check.Statement.Size {
		return ReleaseCheck{}, fleeterror.NewInvalidArgumentErrorf("package size %d does not match the signed release statement (%d)", size, check.Statement.Size)
	}
	if size > s.cfg.MaxPackageSize {
		return ReleaseCheck{}, fleeterror.NewInvalidArgumentErrorf("package size exceeds the maximum allowed size (%d bytes)", s.cfg.MaxPackageSize)
	}
	return check, nil
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

// checkRelease checks a release statement and its signature.
//
// When Fleet has installed release keys, the signature must verify against one
// of them, and publicKey is not used: a key from the uploader cannot replace
// the keys an operator installed.
//
// When Fleet has no installed release key, the upload is still accepted. The
// signature is checked against publicKey when one is supplied. If none is
// supplied, or the signature does not verify with it, the result is
// SignatureUnverified with a warning, and the controllers decide. Fleet's check
// was never the security boundary: every controller verifies the signature
// against its own trusted keys before it stops anything.
//
// A key directory that cannot be read, or that holds a file that does not
// parse, is an error and never falls back to publicKey, so a bad key rotation
// does not turn the installed-key check off.
func (s *Service) checkRelease(statement, signature, publicKey []byte) (ReleaseCheck, error) {
	if s.verifier != nil {
		verified, err := s.verifier.Verify(statement, signature, "")
		switch {
		case err == nil:
			return ReleaseCheck{Statement: verified.Statement, KeyID: verified.KeyID, Signature: SignatureInstalledKey}, nil
		case errors.Is(err, frigaterelease.ErrNoTrustedKeys):
			// The key directory is empty: continue without an installed key.
		case errors.Is(err, frigaterelease.ErrSignatureInvalid),
			errors.Is(err, frigaterelease.ErrKeyIDMismatch),
			errors.Is(err, frigaterelease.ErrStatementInvalid):
			return ReleaseCheck{}, fleeterror.NewInvalidArgumentErrorf("release statement rejected: %v", err)
		default:
			slog.Error("frigate release verification failed", "error", err)
			return ReleaseCheck{}, fleeterror.NewInternalError("failed to verify the release statement")
		}
	}
	return checkReleaseWithoutInstalledKey(statement, signature, publicKey)
}

// checkReleaseWithoutInstalledKey is checkRelease for a Fleet that has no
// installed release key. The statement must still be canonical and the
// signature within the size limit: Fleet reads the package facts from the
// statement, and the controllers refuse a manifest that breaks either rule.
func checkReleaseWithoutInstalledKey(statement, signature, publicKey []byte) (ReleaseCheck, error) {
	if len(signature) == 0 || len(signature) > frigaterelease.MaxSignatureBytes {
		return ReleaseCheck{}, fleeterror.NewInvalidArgumentError(errSignatureFile)
	}
	parsed, err := frigaterelease.ParseStatement(statement)
	if err != nil {
		return ReleaseCheck{}, fleeterror.NewInvalidArgumentErrorf("release statement rejected: %v", err)
	}
	check := ReleaseCheck{Statement: parsed, Signature: SignatureUnverified, Warning: warnNoReleaseKey}
	if len(publicKey) == 0 {
		return check, nil
	}
	// A file that is not a release public key is a mistake in the upload, not
	// a signature Fleet could not confirm, so it is refused. Too large, not
	// PEM, not a public key and not P-256 all get the same short message.
	if len(publicKey) > MaxReleasePublicKeyBytes {
		return ReleaseCheck{}, fleeterror.NewInvalidArgumentError(errPublicKeyFile)
	}
	keys, err := frigaterelease.ParsePublicKeys(publicKey)
	if err != nil || len(keys) == 0 {
		return ReleaseCheck{}, fleeterror.NewInvalidArgumentError(errPublicKeyFile)
	}
	verified, err := frigaterelease.NewStaticVerifier(keys...).Verify(statement, signature, "")
	switch {
	case err == nil:
		check.KeyID, check.Signature, check.Warning = verified.KeyID, SignatureUploadedKey, ""
	case errors.Is(err, frigaterelease.ErrSignatureInvalid):
		check.Warning = warnReleaseKeyMismatch
	default:
		return ReleaseCheck{}, fleeterror.NewInvalidArgumentErrorf("release statement rejected: %v", err)
	}
	return check, nil
}

// ImportInput describes a fully uploaded package waiting in the staging area.
type ImportInput struct {
	// StagedPath is the staged file created by FrigatePackageStore.NewStagingPath.
	// ImportPackage consumes it: on return it is either published or removed.
	StagedPath string
	Filename   string
	Statement  []byte
	Signature  []byte
	// PublicKey is the optional PEM release public key supplied with the
	// upload. It is used only when Fleet has no installed release key.
	PublicKey []byte
}

// ImportResult is the outcome of ImportPackage.
type ImportResult struct {
	Package Package
	// Reused is true when an identical package was already stored.
	Reused bool
	// Warning is operator-facing text. It is set when the stored package has no
	// signature that Fleet verified.
	Warning string
}

// importResult builds the result for a stored package row. The warning follows
// the stored record, not this upload: a package that was verified earlier stays
// verified when it is uploaded again without a key.
func importResult(row sqlc.FrigateUpdatePackage, uploader string, reused bool, check ReleaseCheck) ImportResult {
	result := ImportResult{Package: packageFromModel(row, uploader), Reused: reused}
	if row.ReleaseKeyID == "" {
		result.Warning = check.Warning
	}
	return result
}

// ImportPackage checks an uploaded package against its release statement and
// stores it. It checks the signature as checkRelease describes, and always
// checks the file size and SHA-256 against the statement and the Debian control
// data against the statement.
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
	check, err := s.checkRelease(in.Statement, in.Signature, in.PublicKey)
	if err != nil {
		return ImportResult{}, err
	}
	statement := check.Statement
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
		existing, err = s.refreshRelease(ctx, existing, check, in)
		if err != nil {
			return ImportResult{}, err
		}
		if !s.store.Exists(existing.ID, existing.SizeBytes) {
			// The row survived but the blob did not (restored database, new
			// host). Republish the checked bytes under the existing ID.
			if err := s.store.Publish(existing.ID, in.StagedPath); err != nil {
				return ImportResult{}, fleeterror.NewInternalErrorf("failed to store the package: %v", err)
			}
		}
		return importResult(existing, "", true, check), nil
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
		Repository: statement.Repository, ReleaseKeyID: check.KeyID, MinInstalledVersion: statement.MinInstalledVersion,
		IssuedAt: statement.IssuedAt, Statement: in.Statement, Signature: in.Signature,
		UploadedBy: sql.NullInt64{Int64: info.UserID, Valid: true},
	})
	if err != nil {
		_ = s.store.Delete(id)
		if db.IsUniqueViolationError(err) {
			// A concurrent upload of the same package won the race.
			winner, getErr := s.queries.GetFrigateUpdatePackageBySHA256(ctx, sqlc.GetFrigateUpdatePackageBySHA256Params{OrgID: info.OrganizationID, Sha256: digest})
			if getErr == nil {
				return importResult(winner, "", true, check), nil
			}
		}
		return ImportResult{}, fleeterror.NewInternalErrorf("failed to record the package: %v", err)
	}

	description := fmt.Sprintf("Uploaded controller software %s (%s)", statement.Version, statement.Architecture)
	if check.Signature == SignatureUnverified {
		description += ", signature not verified"
	}
	s.logPackageEvent(ctx, info.OrganizationID, packageUploadedEventType, description, row, info.ExternalUserID, info.Username, check.Signature)
	return importResult(row, info.Username, false, check), nil
}

// refreshRelease applies a freshly checked release statement to a package that
// is already stored. The bytes are identical, so only the statement-derived
// fields can differ, for example after a key rotation re-signed the release.
// A re-upload of the same statement and key keeps the stored signature: ECDSA
// signatures differ on every signing, and both verify. A package that a
// blocking job references keeps its statement, because the controllers hold the
// manifest Fleet built from it; replacing it then is refused.
//
// An upload that Fleet did not verify never replaces a release that Fleet did
// verify. With the same statement the stored, verified signature is kept. With
// a different statement the upload is refused, because Fleet cannot tell
// whether the new statement is genuine.
//
// The decision reads the locked row, not existing: a concurrent upload of the
// same package can change the row after ImportPackage read it.
func (s *Service) refreshRelease(ctx context.Context, existing sqlc.FrigateUpdatePackage, check ReleaseCheck, in ImportInput) (sqlc.FrigateUpdatePackage, error) {
	statement := check.Statement
	updated, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.FrigateUpdatePackage, error) {
		locked, err := q.LockFrigateUpdatePackage(ctx, sqlc.LockFrigateUpdatePackageParams{OrgID: existing.OrgID, ID: existing.ID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return sqlc.FrigateUpdatePackage{}, fleeterror.NewFailedPreconditionError("the package was deleted while it was being uploaded; upload it again")
			}
			return sqlc.FrigateUpdatePackage{}, err
		}
		sameStatement := bytes.Equal(locked.Statement, in.Statement)
		switch {
		case sameStatement && locked.ReleaseKeyID == check.KeyID && check.KeyID != "":
			return locked, nil
		case sameStatement && check.KeyID == "" && (locked.ReleaseKeyID != "" || bytes.Equal(locked.Signature, in.Signature)):
			return locked, nil
		case check.KeyID == "" && locked.ReleaseKeyID != "":
			return sqlc.FrigateUpdatePackage{}, fleeterror.NewFailedPreconditionError(errVerifiedStored)
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
			Repository: statement.Repository, ReleaseKeyID: check.KeyID, MinInstalledVersion: statement.MinInstalledVersion,
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

// logPackageEvent records a package event. signature is how Fleet checked the
// release signature at upload; it is empty for events that are not an upload.
func (s *Service) logPackageEvent(ctx context.Context, orgID int64, eventType, description string, pkg sqlc.FrigateUpdatePackage, externalID, username string, signature SignatureCheck) {
	metadata := map[string]any{
		"package_id":   pkg.ID.String(),
		"version":      pkg.Version,
		"architecture": pkg.Architecture,
		"sha256":       pkg.Sha256,
		"filename":     pkg.Filename,
	}
	if signature != "" {
		metadata["signature_check"] = string(signature)
	}
	event := activitymodels.Event{
		Category:       activitymodels.CategorySystem,
		Type:           eventType,
		Description:    description,
		ActorType:      activitymodels.ActorUser,
		UserID:         &externalID,
		Username:       &username,
		OrganizationID: &orgID,
		Metadata:       metadata,
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
		fmt.Sprintf("Deleted controller software %s (%s)", deleted.Version, deleted.Architecture), deleted, info.ExternalUserID, info.Username, "")
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
