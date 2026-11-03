package poolconfigurations

import (
	"context"
	"database/sql"
	"encoding/json"

	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

type Access int

const (
	Read Access = iota
	Manage
	// Observe holds shared authority locks through device readback; it cannot authorize a library mutation.
	Observe
)

type Principal struct {
	Scope                  Scope
	ActorID                int64
	UserID, Username       string
	AccountID, AccountName string
}

// Authority resolves a selected external account into its private owner scope,
// retaining live actor/permission locks until the transaction completes.
type Authority interface {
	AuthorizePoolConfiguration(ctx context.Context, q sqlc.Querier, accountID string, access Access) (Principal, error)
	PoolEquipmentVisibility(ctx context.Context, q sqlc.Querier, principal Principal) (func(siteID sql.NullInt64) bool, error)
}

// CredentialProof binds a password verification to the live actor credential.
// Hash verification runs outside transactions; this proof is checked under the
// actor lock immediately before saving the reviewed change.
type CredentialProof func(ctx context.Context, q sqlc.Querier) error

type Equipment interface {
	TargetReviewer
	VerifyPoolEquipmentCredentials(ctx context.Context, accountID, password string) (CredentialProof, error)
}

type Service struct {
	checker          ConfigurationChecker
	conn             *sql.DB
	repository       *Repository
	authority        Authority
	equipment        Equipment
	selector         DeviceSelector
	readiness        Readiness
	connectionTester ConnectionTester
}

type Readiness interface {
	RefreshTargets(ctx context.Context, scope Scope, actorID int64, targets []AssignmentTarget) error
}

func (s *Service) WithReadiness(readiness Readiness) *Service { s.readiness = readiness; return s }

type DeviceSelector interface {
	ResolveDeviceIdentifiers(ctx context.Context, selector *fleetpb.DeviceSelector, orgID int64) ([]string, error)
}

func (s *Service) WithDeviceSelector(selector DeviceSelector) *Service {
	s.selector = selector
	return s
}

func NewService(conn *sql.DB, repository *Repository, authority Authority, equipment Equipment) *Service {
	return &Service{conn: conn, repository: repository, authority: authority, equipment: equipment}
}

func withAuthority[T any](ctx context.Context, s *Service, accountID string, access Access, fn func(sqlc.Querier, Principal) (T, error)) (T, error) {
	return db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (T, error) {
		var zero T
		if s.authority == nil {
			return zero, fleeterror.NewInternalError("configuration authority is unavailable")
		}
		principal, err := s.authority.AuthorizePoolConfiguration(ctx, q, accountID, access)
		if err != nil {
			return zero, err
		}
		if principal.ActorID <= 0 || principal.Scope.validate() != nil {
			return zero, fleeterror.NewInternalError("invalid configuration authority")
		}
		if access == Manage {
			frozen, err := q.PoolConfigurationWritersFrozen(ctx, principal.Scope.OrganizationID)
			if err != nil {
				return zero, err
			}
			if frozen {
				return zero, fleeterror.NewFailedPreconditionError("Pool configuration maintenance is in progress.")
			}
		}
		return fn(q, principal)
	})
}

func (s *Service) Create(ctx context.Context, accountID string, options RevisionOptions) (Configuration, error) {
	return withAuthority(ctx, s, accountID, Manage, func(q sqlc.Querier, p Principal) (Configuration, error) {
		config, err := s.repository.Create(ctx, q, p.Scope, options)
		if err != nil {
			return Configuration{}, err
		}
		if err := configurationAudit(ctx, q, p, "create_pool_configuration", config.ID, config.RevisionID); err != nil {
			return Configuration{}, err
		}
		return config, nil
	})
}

func (s *Service) Get(ctx context.Context, accountID, id string) (Summary, error) {
	return withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) (Summary, error) { return s.repository.Get(ctx, q, p.Scope, id) })
}

func (s *Service) List(ctx context.Context, accountID, pageToken string, pageSize int32, includeArchived bool) (Page, error) {
	return withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) (Page, error) {
		return s.repository.List(ctx, q, p.Scope, pageToken, pageSize, includeArchived)
	})
}

func (s *Service) Duplicate(ctx context.Context, accountID, id, name string) (Configuration, error) {
	return withAuthority(ctx, s, accountID, Manage, func(q sqlc.Querier, p Principal) (Configuration, error) {
		config, err := s.repository.Duplicate(ctx, q, p.Scope, id, name)
		if err != nil {
			return Configuration{}, err
		}
		if err := configurationAudit(ctx, q, p, "duplicate_pool_configuration", config.ID, config.RevisionID); err != nil {
			return Configuration{}, err
		}
		return config, nil
	})
}

func (s *Service) Archive(ctx context.Context, accountID, id string, version int64) error {
	_, err := withAuthority(ctx, s, accountID, Manage, func(q sqlc.Querier, p Principal) (struct{}, error) {
		if err := s.repository.Archive(ctx, q, p.Scope, id, version); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, configurationAudit(ctx, q, p, "archive_pool_configuration", id, "")
	})
	return err
}

func configurationAudit(ctx context.Context, q sqlc.Querier, p Principal, event, configID, revisionID string) error {
	metadata := map[string]string{"configuration_id": configID, "revision_id": revisionID}
	scopeType, scopeLabel := "organization", ""
	if p.Scope.AccountID != 0 {
		metadata["account_id"] = p.AccountID
		scopeType, scopeLabel = "account", p.AccountName
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fleeterror.NewInternalError("encode configuration audit failed")
	}
	err = q.InsertActivityLog(ctx, sqlc.InsertActivityLogParams{EventID: uuid.New(), EventCategory: string(models.CategoryPool), EventType: event, Description: event, Result: string(models.ResultSuccess), ActorType: string(models.ActorUser), UserID: sql.NullString{String: p.UserID, Valid: true}, Username: sql.NullString{String: p.Username, Valid: true}, OrganizationID: sql.NullInt64{Int64: p.Scope.OrganizationID, Valid: true}, ScopeType: sql.NullString{String: scopeType, Valid: true}, ScopeLabel: sql.NullString{String: scopeLabel, Valid: scopeLabel != ""}, Metadata: pqtype.NullRawMessage{RawMessage: encoded, Valid: true}, MemberSiteIds: []int64{}})
	if err != nil {
		return storageError(err)
	}
	return nil
}

// ReviewEdit captures proposed changes without changing saved settings. A
// service without equipment integration refuses assigned settings changes.
func (s *Service) ReviewEdit(ctx context.Context, accountID, id string, version int64, options RevisionOptions) (Review, error) {
	if err := s.refreshEdit(ctx, accountID, id, options); err != nil {
		return Review{}, err
	}
	return withAuthority(ctx, s, accountID, Manage, func(q sqlc.Querier, p Principal) (Review, error) {
		review, err := s.repository.ReviewEdit(ctx, q, p.Scope, p.ActorID, id, version, options, s.equipment)
		if err != nil {
			return Review{}, err
		}
		summary, err := s.repository.Get(ctx, q, p.Scope, id)
		review.AssignedCount = summary.AssignedCount
		return review, err
	})
}
