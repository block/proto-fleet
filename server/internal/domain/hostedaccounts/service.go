// Package hostedaccounts owns customer accounts independently of organization
// RBAC. It is an internal foundation; customer RPCs and asset access are not
// exposed until their account scope is enforced end to end.
package hostedaccounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type Capabilities int64

const (
	View Capabilities = 1 << iota
	ManagePools
	ChangePools
	ManageMembers
	Delegate

	DefaultCapabilities = View | ManagePools | ChangePools | ManageMembers
	AllCapabilities     = DefaultCapabilities | Delegate
)

type Role string

const (
	RoleViewer        Role = "viewer"
	RolePoolOperator  Role = "pool_operator"
	RoleAdministrator Role = "account_administrator"
)

func (r Role) capabilities() Capabilities {
	switch r {
	case RoleViewer:
		return View
	case RolePoolOperator:
		return View | ManagePools | ChangePools
	case RoleAdministrator:
		return AllCapabilities
	default:
		return 0
	}
}

type Account = sqlc.HostedAccount

type Access struct {
	AccountID    string
	Role         Role
	Capabilities Capabilities
}

func (a Access) Has(required Capabilities) bool {
	return required > 0 && required&^AllCapabilities == 0 && a.Capabilities&required == required
}

type Service struct {
	conn    *sql.DB
	queries sqlc.Querier
}

func NewService(conn *sql.DB) *Service {
	return &Service{conn: conn, queries: db.NewFailoverResettingQuerier(db.NewRetryDB(conn))}
}

// Resolve combines direct membership and explicit ancestor grants in one live
// snapshot. Physical custody and ancestor membership alone confer no access.
func (s *Service) Resolve(ctx context.Context, userID, orgID int64, accountID string) (Access, error) {
	return resolve(ctx, s.queries, userID, orgID, accountID)
}

func resolve(ctx context.Context, q sqlc.Querier, userID, orgID int64, accountID string) (Access, error) {
	row, err := q.ResolveHostedAccountAuthority(ctx, sqlc.ResolveHostedAccountAuthorityParams{
		OrganizationID: orgID, AccountID: accountID, UserID: userID,
	})
	if err != nil {
		return Access{}, accessError(err)
	}
	role := Role(row.Role)
	capabilities := Capabilities(row.Capabilities)
	if capabilities&View == 0 {
		return Access{}, denied()
	}
	return Access{AccountID: row.AccountID, Role: role, Capabilities: capabilities}, nil
}

// CreateChild atomically creates a client/subclient and its initial administrator.
// Names are display labels; identities and memberships are independent of names.
func (s *Service) CreateChild(ctx context.Context, actorID, orgID int64, parentID, name string, adminID int64, capabilities Capabilities) (Account, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 255 {
		return Account{}, fleeterror.NewInvalidArgumentError("account name must contain 1 to 255 characters")
	}
	if err := validateCapabilities(capabilities); err != nil {
		return Account{}, err
	}
	return db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (Account, error) {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return Account{}, err
		}
		path, err := loadPath(ctx, q, orgID, parentID)
		if err != nil {
			return Account{}, err
		}
		ceiling, err := authorize(ctx, q, actorID, orgID, path, Delegate)
		if err != nil {
			return Account{}, err
		}
		parent := path[len(path)-1]
		if parent.Depth >= 2 {
			return Account{}, fleeterror.NewFailedPreconditionError("account hierarchy is limited to host, client, and subclient")
		}
		if capabilities&^ceiling != 0 {
			return Account{}, fleeterror.NewForbiddenError("cannot delegate capabilities excluded by the parent")
		}
		if _, err := q.LockActiveHostedAccountUser(ctx, sqlc.LockActiveHostedAccountUserParams{ID: adminID, OrganizationID: orgID}); err != nil {
			return Account{}, accessError(err)
		}
		account, err := q.CreateHostedAccount(ctx, sqlc.CreateHostedAccountParams{
			AccountID: uuid.NewString(), OrganizationID: orgID,
			ParentID: sql.NullInt64{Int64: parent.ID, Valid: true}, Depth: parent.Depth + 1,
			Name: name, Capabilities: int64(capabilities),
		})
		if err != nil {
			return Account{}, fleeterror.NewInternalErrorf("create account: %w", err)
		}
		if err := q.SetHostedAccountMembership(ctx, sqlc.SetHostedAccountMembershipParams{OrganizationID: orgID, AccountID: account.ID, UserID: adminID, Role: string(RoleAdministrator)}); err != nil {
			return Account{}, err
		}
		if err := audit(ctx, q, actor, account, "create_hosted_account", map[string]any{"administrator_user_id": adminID, "capabilities": capabilities}); err != nil {
			return Account{}, err
		}
		return account, nil
	})
}

// ConfigureChild changes only the policy of a directly delegated child. It
// does not grant access to the child's assets, membership roster, or pools.
func (s *Service) ConfigureChild(ctx context.Context, actorID, orgID int64, accountID string, capabilities Capabilities, suspended bool) error {
	if err := validateCapabilities(capabilities); err != nil {
		return err
	}
	return db.WithTransactionNoResult(ctx, s.conn, func(q sqlc.Querier) error {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return err
		}
		path, err := loadPath(ctx, q, orgID, accountID)
		if err != nil {
			return err
		}
		if len(path) < 2 {
			return denied()
		}
		ceiling, err := authorize(ctx, q, actorID, orgID, path[:len(path)-1], Delegate)
		if err != nil {
			return err
		}
		if capabilities&^ceiling != 0 {
			return fleeterror.NewForbiddenError("cannot restore capabilities excluded by the parent")
		}
		account := path[len(path)-1]
		if err := q.ConfigureHostedAccount(ctx, sqlc.ConfigureHostedAccountParams{Capabilities: int64(capabilities), Suspended: suspended, OrganizationID: orgID, ID: account.ID}); err != nil {
			return err
		}
		account.Revision++
		if err := requireAdministrators(ctx, q, orgID); err != nil {
			return err
		}
		return audit(ctx, q, actor, account, "configure_hosted_account", map[string]any{"previous_capabilities": account.Capabilities, "capabilities": capabilities, "previous_suspended": account.Suspended, "suspended": suspended})
	})
}

// SetMembership manages existing affiliated users without creating global
// identities or organization grants. It is not exposed as a customer add-user
// API; invitation acceptance will need its own atomic consumption transaction.
func (s *Service) SetMembership(ctx context.Context, actorID, orgID int64, accountID string, userID int64, role Role) error {
	if role.capabilities() == 0 {
		return fleeterror.NewInvalidArgumentError("invalid account role")
	}
	return s.changeMembership(ctx, actorID, orgID, accountID, userID, &role)
}

func (s *Service) RevokeMembership(ctx context.Context, actorID, orgID int64, accountID string, userID int64) error {
	return s.changeMembership(ctx, actorID, orgID, accountID, userID, nil)
}

func (s *Service) changeMembership(ctx context.Context, actorID, orgID int64, accountID string, userID int64, role *Role) error {
	return db.WithTransactionNoResult(ctx, s.conn, func(q sqlc.Querier) error {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return err
		}
		path, err := loadPath(ctx, q, orgID, accountID)
		if err != nil {
			return err
		}
		_, err = authorize(ctx, q, actorID, orgID, path, ManageMembers)
		if err != nil {
			return err
		}
		if role != nil {
			if err := requireRoleGrant(ctx, q, actorID, orgID, accountID, *role); err != nil {
				return err
			}
		}
		account := path[len(path)-1]
		if account.Depth == 0 {
			return denied()
		}
		previous, err := q.GetHostedAccountMembership(ctx, sqlc.GetHostedAccountMembershipParams{OrganizationID: orgID, AccountID: account.ID, UserID: userID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		metadata := map[string]any{"member_user_id": userID, "previous_role": previous.Role}
		event := "revoke_hosted_account_membership"
		if role != nil {
			if _, err := q.LockActiveHostedAccountUser(ctx, sqlc.LockActiveHostedAccountUserParams{ID: userID, OrganizationID: orgID}); err != nil {
				return accessError(err)
			}
			if err := q.SetHostedAccountMembership(ctx, sqlc.SetHostedAccountMembershipParams{OrganizationID: orgID, AccountID: account.ID, UserID: userID, Role: string(*role)}); err != nil {
				return err
			}
			event = "set_hosted_account_membership"
			metadata["role"] = *role
		} else {
			if errors.Is(err, sql.ErrNoRows) {
				return denied()
			}
			if err := q.RevokeHostedAccountMembership(ctx, sqlc.RevokeHostedAccountMembershipParams{OrganizationID: orgID, AccountID: account.ID, UserID: userID}); err != nil {
				return err
			}
		}
		if err := requireAdministrators(ctx, q, orgID); err != nil {
			return err
		}
		return audit(ctx, q, actor, account, event, metadata)
	})
}

func lockActor(ctx context.Context, q sqlc.Querier, orgID, actorID int64) (sqlc.User, error) {
	if _, err := q.LockHostedAccountOrganization(ctx, orgID); err != nil {
		return sqlc.User{}, accessError(err)
	}
	actor, err := q.LockActiveHostedAccountUser(ctx, sqlc.LockActiveHostedAccountUserParams{ID: actorID, OrganizationID: orgID})
	if err != nil {
		return sqlc.User{}, accessError(err)
	}
	return actor, nil
}

func loadPath(ctx context.Context, q sqlc.Querier, orgID int64, accountID string) ([]sqlc.HostedAccount, error) {
	rows, err := q.GetHostedAccountPath(ctx, sqlc.GetHostedAccountPathParams{OrganizationID: orgID, AccountID: accountID})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("load account path: %w", err)
	}
	path := make([]sqlc.HostedAccount, len(rows))
	for i, row := range rows {
		path[i] = sqlc.HostedAccount(row)
	}
	if len(path) == 0 || path[0].Depth != 0 || int(path[len(path)-1].Depth)+1 != len(path) {
		return nil, denied()
	}
	return path, nil
}

func authorize(ctx context.Context, q sqlc.Querier, actorID, orgID int64, path []sqlc.HostedAccount, required Capabilities) (Capabilities, error) {
	ceiling := AllCapabilities
	for _, account := range path {
		if account.Suspended {
			return 0, denied()
		}
		ceiling &= Capabilities(account.Capabilities)
	}
	account := path[len(path)-1]
	if account.Depth == 0 {
		// Root control is a host permission, never inherited customer access.
		live, err := q.HasHostedAccountHostAuthority(ctx, sqlc.HasHostedAccountHostAuthorityParams{UserID: actorID, OrganizationID: orgID})
		if err != nil {
			return 0, err
		}
		effective, err := authz.LoadEffectiveForUpdate(ctx, q, actorID, orgID)
		if err != nil {
			return 0, err
		}
		if required != Delegate || !live || !effective.Has(authz.PermAccountManage, authz.ResourceContext{}) {
			return 0, denied()
		}
		return ceiling, nil
	}
	access, err := resolve(ctx, q, actorID, orgID, account.AccountID)
	if err != nil {
		return 0, err
	}
	if !access.Has(required) {
		return 0, denied()
	}
	return access.Capabilities, nil
}

func validateCapabilities(capabilities Capabilities) error {
	// Both direct and explicitly granted administration remain subject to the
	// hierarchy ceiling. Removing this bit would leave no recovery authority.
	if capabilities&^AllCapabilities != 0 || capabilities&(View|ManageMembers) != View|ManageMembers {
		return fleeterror.NewInvalidArgumentError("account capabilities must permit viewing and membership administration")
	}
	return nil
}

func denied() error { return fleeterror.NewForbiddenError("account access denied") }

func accessError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return denied()
	}
	return fleeterror.NewInternalErrorf("resolve account authority: %w", err)
}

func audit(ctx context.Context, q sqlc.Querier, actor sqlc.User, account Account, event string, metadata map[string]any) error {
	metadata["account_id"] = account.AccountID
	metadata["account_revision"] = account.Revision
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fleeterror.NewInternalErrorf("encode account audit metadata: %w", err)
	}
	return q.InsertActivityLog(ctx, sqlc.InsertActivityLogParams{
		EventID: uuid.New(), EventCategory: "auth", EventType: event, Description: strings.ReplaceAll(event, "_", " "),
		Result: "success", ActorType: "user",
		UserID: sql.NullString{String: actor.UserID, Valid: true}, Username: sql.NullString{String: actor.Username, Valid: true},
		OrganizationID: sql.NullInt64{Int64: account.OrganizationID, Valid: true},
		ScopeType:      sql.NullString{String: "account", Valid: true}, ScopeLabel: sql.NullString{String: account.Name, Valid: true},
		Metadata: pqtype.NullRawMessage{RawMessage: encoded, Valid: true}, MemberSiteIds: []int64{},
	})
}

// Fixed roles persist after hierarchy ceilings change. Check their full
// capabilities against live membership/grant sources before those ceilings.
// Callers separately require effective membership-management authority.
func requireRoleGrant(ctx context.Context, q sqlc.Querier, actorID, orgID int64, accountID string, role Role) error {
	authority, err := q.ResolveHostedAccountAuthority(ctx, sqlc.ResolveHostedAccountAuthorityParams{OrganizationID: orgID, AccountID: accountID, UserID: actorID})
	if err != nil {
		return accessError(err)
	}
	if role.capabilities() & ^Capabilities(authority.SourceCapabilities) != 0 {
		return fleeterror.NewForbiddenError("cannot grant a role beyond current account authority")
	}
	return nil
}
