package poolconfigurations

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"slices"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/google/uuid"
)

// Scope identifies the owner, not the actor. AccountID zero denotes the operator
// organization. Private account authority is resolved before entering Repository.
type Scope struct {
	OrganizationID int64
	AccountID      int64
}

func (s Scope) account() sql.NullInt64 {
	return sql.NullInt64{Int64: s.AccountID, Valid: s.AccountID != 0}
}

func (s Scope) validate() error {
	if s.OrganizationID <= 0 || s.AccountID < 0 {
		return fleeterror.NewInvalidArgumentError("invalid pool configuration owner")
	}
	return nil
}

// Endpoint is safe to return to an authorized reader: it contains no credential.
type Endpoint struct {
	ID, URL, Username, WorkerTemplate string
	HasPassword                       bool
}

type Configuration struct {
	ID, Name, RevisionID string
	Version              int64
	Archived             bool
	Slots                []Endpoint
}

type SaveResult struct {
	Configuration   Configuration
	SettingsChanged bool
}

// Repository participates in the caller's transaction. It does not authorize
// users or dispatch equipment updates. The caller must admit the owner scope and
// commit reviewed assignments, durable work and audit together with a revision.
type Repository struct {
	secrets *encrypt.Service
}

func NewRepository(secrets *encrypt.Service) *Repository {
	return &Repository{secrets: secrets}
}

func (r *Repository) Create(ctx context.Context, q sqlc.Querier, scope Scope, options RevisionOptions) (Configuration, error) {
	if err := scope.validate(); err != nil {
		return Configuration{}, err
	}
	options, err := Validate(options)
	if err != nil {
		return Configuration{}, err
	}
	options.Slots = slices.Clone(options.Slots)
	for i := range options.Slots {
		options.Slots[i].ID = uuid.NewString()
	}
	slots, err := EncryptSlots(r.secrets, options.Slots, nil)
	if err != nil {
		return Configuration{}, err
	}
	config, err := q.CreatePoolConfiguration(ctx, sqlc.CreatePoolConfigurationParams{
		ConfigurationID: uuid.NewString(), OrganizationID: scope.OrganizationID, AccountID: scope.account(), Name: options.Name,
	})
	if err != nil {
		return Configuration{}, storageError(err)
	}
	revision, err := q.CreatePoolConfigurationRevision(ctx, sqlc.CreatePoolConfigurationRevisionParams{
		RevisionID: uuid.NewString(), OrganizationID: scope.OrganizationID, ConfigurationID: config.ConfigurationID, Number: 1, Slots: slots,
	})
	if err != nil {
		return Configuration{}, storageError(err)
	}
	config, err = q.SetPoolConfigurationCurrentRevision(ctx, sqlc.SetPoolConfigurationCurrentRevisionParams{
		OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: config.ConfigurationID,
		RevisionID: sql.NullString{String: revision.RevisionID, Valid: true}, ExpectedVersion: config.Version, Name: options.Name,
	})
	if err != nil {
		return Configuration{}, storageError(err)
	}
	return publicConfiguration(config, slots)
}

// Revise persists trusted options, including new slot IDs issued during review.
// External edits must go through ReviewEdit to validate existing slot ownership.
func (r *Repository) Revise(ctx context.Context, q sqlc.Querier, scope Scope, id string, expectedVersion int64, options RevisionOptions) (SaveResult, error) {
	if err := scope.validate(); err != nil {
		return SaveResult{}, err
	}
	options, err := Validate(options)
	if err != nil {
		return SaveResult{}, err
	}
	config, err := q.LockPoolConfiguration(ctx, sqlc.LockPoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id})
	if err != nil {
		return SaveResult{}, storageError(err)
	}
	if config.Archived || config.Version != expectedVersion {
		return SaveResult{}, fleeterror.NewFailedPreconditionError("configuration changed; refresh and review again")
	}
	previous, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{
		OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id, RevisionID: config.CurrentRevisionID.String,
	})
	if err != nil {
		return SaveResult{}, storageError(err)
	}
	oldSlots, err := DecodeSlots(previous.Slots)
	if err != nil {
		return SaveResult{}, err
	}
	options.Slots = slices.Clone(options.Slots)
	for i := range options.Slots {
		if options.Slots[i].ID == "" {
			options.Slots[i].ID = uuid.NewString()
		}
	}
	encoded, err := EncryptSlots(r.secrets, options.Slots, oldSlots)
	if err != nil {
		return SaveResult{}, err
	}
	nextSlots, err := DecodeSlots(encoded)
	if err != nil {
		return SaveResult{}, err
	}
	equal, err := r.Equivalent(oldSlots, nextSlots)
	if err != nil {
		return SaveResult{}, err
	}
	if equal && config.Name == options.Name {
		public, err := publicConfiguration(config, previous.Slots)
		return SaveResult{Configuration: public}, err
	}
	revisionID := previous.RevisionID
	if !equal {
		revision, err := q.CreatePoolConfigurationRevision(ctx, sqlc.CreatePoolConfigurationRevisionParams{
			RevisionID: uuid.NewString(), OrganizationID: scope.OrganizationID, ConfigurationID: id, Number: previous.Number + 1, Slots: encoded,
		})
		if err != nil {
			return SaveResult{}, storageError(err)
		}
		revisionID = revision.RevisionID
	} else {
		encoded = previous.Slots
	}
	config, err = q.SetPoolConfigurationCurrentRevision(ctx, sqlc.SetPoolConfigurationCurrentRevisionParams{
		OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id,
		RevisionID: sql.NullString{String: revisionID, Valid: true}, ExpectedVersion: expectedVersion, Name: options.Name,
	})
	if err != nil {
		return SaveResult{}, storageError(err)
	}
	public, err := publicConfiguration(config, encoded)
	return SaveResult{Configuration: public, SettingsChanged: !equal}, err
}

// Equivalent compares effective ordered settings, independent of slot IDs and
// randomized encryption. Plaintext and equality fingerprints are never returned.
func (r *Repository) Equivalent(a, b []StoredSlot) (bool, error) {
	if len(a) != len(b) {
		return false, nil
	}
	for i, slot := range a {
		other := b[i]
		if slot.URL != other.URL || slot.Username != other.Username || slot.WorkerTemplate != other.WorkerTemplate {
			return false, nil
		}
		left, err := r.secrets.Decrypt(slot.PasswordCiphertext)
		if err != nil {
			return false, fleeterror.NewInternalError("read pool credential failed")
		}
		right, err := r.secrets.Decrypt(other.PasswordCiphertext)
		if err != nil {
			return false, fleeterror.NewInternalError("read pool credential failed")
		}
		if subtle.ConstantTimeCompare(left, right) != 1 {
			return false, nil
		}
	}
	return true, nil
}

func publicConfiguration(config sqlc.PoolConfiguration, encoded []byte) (Configuration, error) {
	slots, err := DecodeSlots(encoded)
	if err != nil {
		return Configuration{}, err
	}
	result := Configuration{ID: config.ConfigurationID, Name: config.Name, RevisionID: config.CurrentRevisionID.String, Version: config.Version, Archived: config.Archived, Slots: make([]Endpoint, len(slots))}
	for i, slot := range slots {
		result.Slots[i] = Endpoint{ID: slot.ID, URL: slot.URL, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate, HasPassword: slot.HasPassword}
	}
	return result, nil
}

// Preserve SQLSTATE for transaction retries without formatting database values.
type storageCause struct{ cause error }

func (e storageCause) Error() string { return "database operation failed" }
func (e storageCause) Unwrap() error { return e.cause }

func storageError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewNotFoundError("pool configuration not found")
	}
	return fleeterror.NewInternalErrorf("pool configuration storage failed: %w", storageCause{err})
}
