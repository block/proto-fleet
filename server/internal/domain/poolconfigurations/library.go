package poolconfigurations

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/google/uuid"
)

type Summary struct {
	Configuration
	AssignedCount int64
}

type Page struct {
	Configurations []Summary
	NextPageToken  string
}

type libraryCursor struct {
	Scope           Scope
	IncludeArchived bool
	AfterID         string
}

func (r *Repository) Get(ctx context.Context, q sqlc.Querier, scope Scope, id string) (Summary, error) {
	if err := scope.validate(); err != nil {
		return Summary{}, err
	}
	row, err := q.GetPoolConfiguration(ctx, sqlc.GetPoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id})
	if err != nil {
		return Summary{}, storageError(err)
	}
	public, err := publicConfiguration(sqlc.PoolConfiguration{ConfigurationID: row.ConfigurationID, Name: row.Name, CurrentRevisionID: row.CurrentRevisionID, Version: row.Version, Archived: row.Archived}, row.Slots)
	return Summary{Configuration: public, AssignedCount: row.AssignedCount}, err
}

func (r *Repository) List(ctx context.Context, q sqlc.Querier, scope Scope, pageToken string, pageSize int32, includeArchived bool) (Page, error) {
	if err := scope.validate(); err != nil {
		return Page{}, err
	}
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 100 {
		return Page{}, fleeterror.NewInvalidArgumentError("page size must be between 1 and 100")
	}
	var after string
	if pageToken != "" {
		var cursor libraryCursor
		encoded, err := base64.RawURLEncoding.DecodeString(pageToken)
		if err != nil || json.Unmarshal(encoded, &cursor) != nil || cursor.Scope != scope || cursor.IncludeArchived != includeArchived {
			return Page{}, fleeterror.NewInvalidArgumentError("invalid configuration page token")
		}
		if _, err := uuid.Parse(cursor.AfterID); err != nil {
			return Page{}, fleeterror.NewInvalidArgumentError("invalid configuration page token")
		}
		after = cursor.AfterID
	}
	rows, err := q.ListPoolConfigurations(ctx, sqlc.ListPoolConfigurationsParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), AfterID: after, PageLimit: pageSize + 1, IncludeArchived: includeArchived})
	if err != nil {
		return Page{}, storageError(err)
	}
	page := Page{Configurations: make([]Summary, 0, min(len(rows), int(pageSize)))}
	for i, row := range rows {
		if i == int(pageSize) {
			break
		}
		public, err := publicConfiguration(sqlc.PoolConfiguration{ConfigurationID: row.ConfigurationID, Name: row.Name, CurrentRevisionID: row.CurrentRevisionID, Version: row.Version, Archived: row.Archived}, row.Slots)
		if err != nil {
			return Page{}, err
		}
		page.Configurations = append(page.Configurations, Summary{Configuration: public, AssignedCount: row.AssignedCount})
	}
	if len(rows) > int(pageSize) {
		encoded, err := json.Marshal(libraryCursor{Scope: scope, IncludeArchived: includeArchived, AfterID: page.Configurations[len(page.Configurations)-1].ID})
		if err != nil {
			return Page{}, fleeterror.NewInternalError("encode configuration page token failed")
		}
		page.NextPageToken = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return page, nil
}

func (r *Repository) Archive(ctx context.Context, q sqlc.Querier, scope Scope, id string, expectedVersion int64) error {
	if err := scope.validate(); err != nil {
		return err
	}
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, scope.OrganizationID); err != nil {
		return storageError(err)
	}
	if _, err := q.LockPoolConfiguration(ctx, sqlc.LockPoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id}); err != nil {
		return storageError(err)
	}
	n, err := q.ArchivePoolConfiguration(ctx, sqlc.ArchivePoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id, ExpectedVersion: expectedVersion})
	if err != nil {
		return storageError(err)
	}
	if n != 1 {
		return fleeterror.NewFailedPreconditionError("configuration changed or is still assigned; refresh and reassign equipment before deleting")
	}
	return nil
}

// Duplicate copies complete settings within the same owner scope. It creates
// fresh slot identities and never copies assignments or application history.
func (r *Repository) Duplicate(ctx context.Context, q sqlc.Querier, scope Scope, id, name string) (Configuration, error) {
	if err := scope.validate(); err != nil {
		return Configuration{}, err
	}
	row, err := q.GetPoolConfiguration(ctx, sqlc.GetPoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id})
	if err != nil {
		return Configuration{}, storageError(err)
	}
	stored, err := DecodeSlots(row.Slots)
	if err != nil {
		return Configuration{}, err
	}
	options, reason := r.importOptions(ImportCandidate{Name: name, Slots: stored})
	if reason != "" {
		return Configuration{}, fleeterror.NewFailedPreconditionError("configuration could not be duplicated; check its name and stored credentials")
	}
	return r.Create(ctx, q, scope, options)
}
