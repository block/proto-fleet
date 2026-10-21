package hostedaccounts

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

// TeamInvitation is listing metadata; it never carries the bearer code or its hash.
type TeamInvitation = sqlc.ListOrganizationInvitationsRow

type IssuedTeamInvitation struct {
	Invitation TeamInvitation
	Token      string
}

type TeamInvitationPreview struct {
	OrganizationName string
	RoleName         string
	ExpiresAt        time.Time
}

// TeamMember describes the accepting login after it joins the host team.
type TeamMember struct {
	UserID                 string
	Username               string
	RoleName               string
	Permissions            []string
	RequiresPasswordChange bool
}

type TeamRegistration struct {
	Member TeamMember
	Cookie *http.Cookie
}

// TeamInvitationService issues single-use codes that grant an organization
// role. Codes share the customer invitation format, preview and registration
// admission, and the organization lock taken by membership and role writes.
type TeamInvitationService struct {
	invitationCore
}

func NewTeamInvitationService(accounts *Service, sessions *session.Service) *TeamInvitationService {
	return &TeamInvitationService{newInvitationCore(accounts, sessions)}
}

func (s *TeamInvitationService) Create(ctx context.Context, actorID, orgID int64, roleID string, lifetime time.Duration) (IssuedTeamInvitation, error) {
	lifetime, err := invitationLifetime(lifetime)
	if err != nil {
		return IssuedTeamInvitation{}, err
	}
	parsedRoleID, err := authz.ParseRoleID(roleID)
	if err != nil {
		return IssuedTeamInvitation{}, err
	}
	return db.WithTransaction(ctx, s.accounts.conn, func(q sqlc.Querier) (IssuedTeamInvitation, error) {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return IssuedTeamInvitation{}, err
		}
		role, err := q.GetRoleByIDForUpdate(ctx, parsedRoleID)
		if errors.Is(err, sql.ErrNoRows) {
			return IssuedTeamInvitation{}, fleeterror.NewInvalidArgumentError("invalid role_id")
		}
		if err != nil {
			return IssuedTeamInvitation{}, err
		}
		if !invitableTeamRole(role, orgID) {
			return IssuedTeamInvitation{}, fleeterror.NewInvalidArgumentError("invalid role_id")
		}
		actorEff, err := authz.LoadEffectiveForUpdate(ctx, q, actorID, orgID)
		if err != nil {
			return IssuedTeamInvitation{}, fleeterror.NewInternalErrorf("load inviter permissions: %w", err)
		}
		if err := requireTeamRoleGrant(ctx, q, actorEff, role.ID); err != nil {
			return IssuedTeamInvitation{}, err
		}
		fingerprint, err := teamAuthorityFingerprint(ctx, q, role.ID, actorID, orgID)
		if err != nil {
			return IssuedTeamInvitation{}, err
		}
		var row sqlc.OrganizationInvitation
		token, err := s.issueCode(ctx, q, func(hash string) (err error) {
			row, err = q.CreateOrganizationInvitation(ctx, sqlc.CreateOrganizationInvitationParams{
				InvitationID: uuid.NewString(), OrganizationID: orgID, InviterID: actorID, RoleID: role.ID,
				AuthorityFingerprint: fingerprint, TokenHash: hash, ExpiresAt: time.Now().Add(lifetime),
			})
			return err
		})
		if err != nil {
			return IssuedTeamInvitation{}, err
		}
		if err := teamAudit(ctx, q, actor, orgID, "create_team_invitation", "Created user invitation: "+role.Name, map[string]any{
			"invitation_id": row.InvitationID, "role_id": strconv.FormatInt(role.ID, 10), "role_name": role.Name, "expires_at": row.ExpiresAt,
		}); err != nil {
			return IssuedTeamInvitation{}, err
		}
		return IssuedTeamInvitation{Token: token, Invitation: TeamInvitation{
			InvitationID: row.InvitationID, RoleID: role.ID, RoleName: role.Name, ExpiresAt: row.ExpiresAt,
			CreatedAt: row.CreatedAt, InviterUsername: actor.Username, Available: true,
		}}, nil
	})
}

// List returns a bounded page of every team invitation, newest first. Callers
// enforce user:manage; the page token is bound to the organization.
func (s *TeamInvitationService) List(ctx context.Context, orgID int64, pageSize int32, pageToken string) ([]TeamInvitation, string, error) {
	if pageSize < 0 || pageSize > 100 {
		return nil, "", fleeterror.NewInvalidArgumentError("invitation page size must be between 0 and 100")
	}
	if pageSize == 0 {
		pageSize = 25
	}
	cursorTime, cursorID, err := teamInvitationPageCursor(pageToken, orgID)
	if err != nil {
		return nil, "", err
	}
	rows, err := s.accounts.queries.ListOrganizationInvitations(ctx, sqlc.ListOrganizationInvitationsParams{
		OrganizationID: orgID, CursorTime: cursorTime, CursorID: cursorID, PageLimit: pageSize + 1,
	})
	if err != nil {
		return nil, "", err
	}
	next := len(rows) > int(pageSize)
	if next {
		rows = rows[:pageSize]
	}
	type grant struct{ inviterID, roleID int64 }
	grantable := map[grant]bool{}
	for index := range rows {
		if !rows[index].Available {
			continue
		}
		key := grant{rows[index].InviterID, rows[index].RoleID}
		allowed, checked := grantable[key]
		if !checked {
			if allowed, err = s.inviterCanGrant(ctx, orgID, key.inviterID, key.roleID); err != nil {
				return nil, "", err
			}
			grantable[key] = allowed
		}
		rows[index].Available = allowed
	}
	if !next {
		return rows, "", nil
	}
	last := rows[len(rows)-1]
	payload := fmt.Sprintf("1|%d|%s|%s", orgID, last.CreatedAt.UTC().Format(time.RFC3339Nano), last.InvitationID)
	return rows, base64.RawURLEncoding.EncodeToString([]byte(payload)), nil
}

func (s *TeamInvitationService) inviterCanGrant(ctx context.Context, orgID, inviterID, roleID int64) (bool, error) {
	eff, err := authz.LoadEffectiveTx(ctx, s.accounts.queries, inviterID, orgID)
	if err != nil {
		return false, fleeterror.NewInternalErrorf("load inviter permissions: %w", err)
	}
	err = requireTeamRoleGrant(ctx, s.accounts.queries, eff, roleID)
	if fleeterror.IsForbiddenError(err) {
		return false, nil
	}
	return err == nil, err
}

func teamInvitationPageCursor(token string, orgID int64) (sql.NullTime, string, error) {
	if token == "" {
		return sql.NullTime{}, "", nil
	}
	invalid := fleeterror.NewInvalidArgumentError("invalid invitation page token")
	if len(token) > 512 {
		return sql.NullTime{}, "", invalid
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return sql.NullTime{}, "", invalid
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 4 || parts[0] != "1" || parts[1] != strconv.FormatInt(orgID, 10) {
		return sql.NullTime{}, "", invalid
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[2])
	if err != nil || createdAt.IsZero() {
		return sql.NullTime{}, "", invalid
	}
	if id, err := uuid.Parse(parts[3]); err != nil || id.String() != parts[3] {
		return sql.NullTime{}, "", invalid
	}
	return sql.NullTime{Time: createdAt, Valid: true}, parts[3], nil
}

// Revoke withdraws an unused invitation. Revocation only removes pending
// access, so any user:manage holder may revoke regardless of role parity.
func (s *TeamInvitationService) Revoke(ctx context.Context, actorID, orgID int64, invitationID string) error {
	return db.WithTransactionNoResult(ctx, s.accounts.conn, func(q sqlc.Querier) error {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return err
		}
		changed, err := q.RevokeOrganizationInvitation(ctx, sqlc.RevokeOrganizationInvitationParams{OrganizationID: orgID, InvitationID: invitationID})
		if err != nil {
			return err
		}
		if changed != 1 {
			return unavailableInvitation()
		}
		return teamAudit(ctx, q, actor, orgID, "revoke_team_invitation", "Revoked user invitation", map[string]any{"invitation_id": invitationID})
	})
}

// Delete removes an expired or revoked invitation from the list. Pending codes
// must be revoked first, so deletion never withdraws access by itself.
func (s *TeamInvitationService) Delete(ctx context.Context, actorID, orgID int64, invitationID string) error {
	return db.WithTransactionNoResult(ctx, s.accounts.conn, func(q sqlc.Querier) error {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return err
		}
		deleted, err := q.DeleteOrganizationInvitation(ctx, sqlc.DeleteOrganizationInvitationParams{OrganizationID: orgID, InvitationID: invitationID})
		if err != nil {
			return err
		}
		if deleted != 1 {
			return fleeterror.NewFailedPreconditionError("only expired or revoked invitations can be deleted")
		}
		return teamAudit(ctx, q, actor, orgID, "delete_team_invitation", "Deleted user invitation", map[string]any{"invitation_id": invitationID})
	})
}

// Owns reports whether a well-formed code belongs to a team invitation, in
// any lifecycle state. Malformed codes return the unavailable error.
func (s *TeamInvitationService) Owns(ctx context.Context, token string) (bool, error) {
	hash, err := invitationHash(token)
	if err != nil {
		return false, err
	}
	return s.accounts.queries.OrganizationInvitationExists(ctx, hash)
}

func (s *TeamInvitationService) Preview(ctx context.Context, token string) (TeamInvitationPreview, error) {
	release, err := s.admitPreview(ctx)
	if err != nil {
		return TeamInvitationPreview{}, err
	}
	defer release()
	hash, err := invitationHash(token)
	if err != nil {
		return TeamInvitationPreview{}, err
	}
	return db.WithTransaction(ctx, s.accounts.conn, func(q sqlc.Querier) (TeamInvitationPreview, error) {
		i, err := q.GetOrganizationInvitationByHash(ctx, hash)
		if errors.Is(err, sql.ErrNoRows) {
			return TeamInvitationPreview{}, unavailableInvitation()
		}
		if err != nil {
			return TeamInvitationPreview{}, err
		}
		if _, err := validateTeamInvitation(ctx, q, i, false); err != nil {
			return TeamInvitationPreview{}, err
		}
		return TeamInvitationPreview{OrganizationName: i.OrganizationName, RoleName: i.RoleName, ExpiresAt: i.ExpiresAt}, nil
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
}

// Accept grants the invitation's role to a signed-in login of the same
// organization. Logins that already hold a team role keep it unchanged.
func (s *TeamInvitationService) Accept(ctx context.Context, token string, userID, orgID int64) (TeamMember, error) {
	var joined TeamMember
	err := s.withTeamInvitation(ctx, token, func(q sqlc.Querier, i sqlc.GetOrganizationInvitationByHashRow, role sqlc.Role, inviter sqlc.User) error {
		if i.OrganizationID != orgID {
			return unavailableInvitation()
		}
		member, err := q.LockActiveHostedAccountUser(ctx, sqlc.LockActiveHostedAccountUserParams{ID: userID, OrganizationID: orgID})
		if err != nil {
			return accessError(err)
		}
		if err := acceptTeamInvitation(ctx, q, i, role, inviter, member, false); err != nil {
			return err
		}
		joined, err = teamMember(ctx, q, member, orgID, role)
		return err
	})
	return joined, err
}

// Register creates the login, its organization role, the accepted invitation
// and a session in one transaction. Rejected or replayed codes create none.
func (s *TeamInvitationService) Register(ctx context.Context, token, username, password, userAgent, ipAddress string) (TeamRegistration, error) {
	login, release, err := s.prepareRegistration(ctx, token, username, password, ipAddress, func(ctx context.Context, token string) error {
		_, err := s.Preview(ctx, token)
		return err
	})
	if err != nil {
		return TeamRegistration{}, err
	}
	defer release()
	var result TeamRegistration
	err = s.withTeamInvitation(ctx, token, func(q sqlc.Querier, i sqlc.GetOrganizationInvitationByHashRow, role sqlc.Role, inviter sqlc.User) error {
		createdID, err := createInvitedLogin(ctx, q, login)
		if err != nil {
			return err
		}
		if err := q.CreateUserOrganization(ctx, sqlc.CreateUserOrganizationParams{UserID: createdID, OrganizationID: i.OrganizationID}); err != nil {
			return err
		}
		member, err := q.LockActiveHostedAccountUser(ctx, sqlc.LockActiveHostedAccountUserParams{ID: createdID, OrganizationID: i.OrganizationID})
		if err != nil {
			return err
		}
		if err := acceptTeamInvitation(ctx, q, i, role, inviter, member, true); err != nil {
			return err
		}
		created, err := s.sessions.Create(db.WithTxQueries(ctx, q), createdID, i.OrganizationID, userAgent, ipAddress)
		if err != nil {
			return err
		}
		joined, err := teamMember(ctx, q, member, i.OrganizationID, role)
		if err != nil {
			return err
		}
		result = TeamRegistration{Member: joined, Cookie: s.sessions.CreateCookie(created.SessionID)}
		return nil
	})
	return result, err
}

func teamMember(ctx context.Context, q sqlc.Querier, user sqlc.User, orgID int64, role sqlc.Role) (TeamMember, error) {
	eff, err := authz.LoadEffectiveTx(ctx, q, user.ID, orgID)
	if err != nil {
		return TeamMember{}, fleeterror.NewInternalErrorf("load member permissions: %w", err)
	}
	return TeamMember{UserID: user.UserID, Username: user.Username, RoleName: role.Name, Permissions: eff.Keys(), RequiresPasswordChange: user.RequiresPasswordChange}, nil
}

func teamAuthorityFingerprint(ctx context.Context, q sqlc.Querier, roleID, inviterID, orgID int64) (string, error) {
	return q.GetTeamInvitationAuthorityFingerprint(ctx, sqlc.GetTeamInvitationAuthorityFingerprintParams{
		RoleID: roleID, InviterID: inviterID, OrganizationID: orgID,
	})
}

// withTeamInvitation runs fn after locking the organization and inviter and
// revalidating the invitation, role and inviter authority in that snapshot.
func (s *TeamInvitationService) withTeamInvitation(ctx context.Context, token string, fn func(sqlc.Querier, sqlc.GetOrganizationInvitationByHashRow, sqlc.Role, sqlc.User) error) error {
	hash, err := invitationHash(token)
	if err != nil {
		return err
	}
	initial, err := s.accounts.queries.GetOrganizationInvitationByHash(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return unavailableInvitation()
	}
	if err != nil {
		return err
	}
	if teamInvitationUnavailable(initial) {
		return unavailableInvitation()
	}
	return db.WithTransactionNoResult(ctx, s.accounts.conn, func(q sqlc.Querier) error {
		inviter, err := lockActor(ctx, q, initial.OrganizationID, initial.InviterID)
		if err != nil {
			if fleeterror.IsForbiddenError(err) {
				return unavailableInvitation()
			}
			return err
		}
		i, err := q.GetOrganizationInvitationByHash(ctx, hash)
		if err != nil {
			return err
		}
		role, err := validateTeamInvitation(ctx, q, i, true)
		if err != nil {
			return err
		}
		return fn(q, i, role, inviter)
	})
}

func teamInvitationUnavailable(i sqlc.GetOrganizationInvitationByHashRow) bool {
	return i.RevokedAt.Valid || i.AcceptedAt.Valid || !time.Now().Before(i.ExpiresAt)
}

// validateTeamInvitation confirms the invitation is unused, that neither the
// role nor the inviter's authority changed since issuance, and that the
// inviter could still grant the role. Locking callers must already hold the
// organization lock; previews read one unlocked snapshot.
func validateTeamInvitation(ctx context.Context, q sqlc.Querier, i sqlc.GetOrganizationInvitationByHashRow, lock bool) (sqlc.Role, error) {
	if teamInvitationUnavailable(i) {
		return sqlc.Role{}, unavailableInvitation()
	}
	var role sqlc.Role
	var err error
	if lock {
		role, err = q.GetRoleByIDForUpdate(ctx, i.RoleID)
	} else {
		role, err = q.GetRoleByID(ctx, i.RoleID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return sqlc.Role{}, unavailableInvitation()
	}
	if err != nil {
		return sqlc.Role{}, err
	}
	if !invitableTeamRole(role, i.OrganizationID) {
		return sqlc.Role{}, unavailableInvitation()
	}
	var inviterEff *authz.EffectivePermissions
	if lock {
		inviterEff, err = authz.LoadEffectiveForUpdate(ctx, q, i.InviterID, i.OrganizationID)
	} else {
		inviterEff, err = authz.LoadEffectiveTx(ctx, q, i.InviterID, i.OrganizationID)
	}
	if err != nil {
		return sqlc.Role{}, fleeterror.NewInternalErrorf("load inviter permissions: %w", err)
	}
	fingerprint, err := teamAuthorityFingerprint(ctx, q, role.ID, i.InviterID, i.OrganizationID)
	if err != nil {
		return sqlc.Role{}, err
	}
	if subtle.ConstantTimeCompare([]byte(fingerprint), []byte(i.AuthorityFingerprint)) != 1 {
		return sqlc.Role{}, unavailableInvitation()
	}
	if err := requireTeamRoleGrant(ctx, q, inviterEff, role.ID); err != nil {
		if fleeterror.IsForbiddenError(err) {
			return sqlc.Role{}, unavailableInvitation()
		}
		return sqlc.Role{}, err
	}
	return role, nil
}

func invitableTeamRole(role sqlc.Role, orgID int64) bool {
	return role.OrganizationID.Valid && role.OrganizationID.Int64 == orgID &&
		!role.DeletedAt.Valid && role.BuiltinKey.String != string(authz.BuiltinKeySuperAdmin)
}

// requireTeamRoleGrant applies the same hierarchy as direct role assignment:
// user:manage plus the caller-can-manage-target parity check.
func requireTeamRoleGrant(ctx context.Context, q sqlc.Querier, inviterEff *authz.EffectivePermissions, roleID int64) error {
	if !inviterEff.Has(authz.PermUserManage, authz.ResourceContext{}) {
		return fleeterror.NewForbiddenError("insufficient permissions to invite team members")
	}
	keys, err := q.ListRolePermissionKeys(ctx, roleID)
	if err != nil {
		return err
	}
	return auth.RequireCallerCanManageTarget(inviterEff, authz.NewEffectivePermissions([]authz.Assignment{{
		ScopeType: authz.ScopeOrg, Permissions: keys,
	}}))
}

func acceptTeamInvitation(ctx context.Context, q sqlc.Querier, i sqlc.GetOrganizationInvitationByHashRow, role sqlc.Role, inviter, member sqlc.User, registered bool) error {
	hasRole, err := q.GetUserOrganizationRoleState(ctx, sqlc.GetUserOrganizationRoleStateParams{UserID: member.ID, OrganizationID: i.OrganizationID})
	if err != nil {
		return accessError(err)
	}
	// An invitation cannot silently change an existing team member's role.
	if hasRole {
		return fleeterror.NewFailedPreconditionError("you're already on this team; ask an administrator to change your role")
	}
	changed, err := q.ConsumeOrganizationInvitation(ctx, sqlc.ConsumeOrganizationInvitationParams{
		AcceptedUserID: sql.NullInt64{Int64: member.ID, Valid: true}, InvitationID: i.InvitationID,
	})
	if err != nil {
		return err
	}
	if changed != 1 {
		return unavailableInvitation()
	}
	if err := q.UpdateUserRole(ctx, sqlc.UpdateUserRoleParams{RoleID: sql.NullInt64{Int64: role.ID, Valid: true}, UserID: member.ID, OrganizationID: i.OrganizationID}); err != nil {
		return err
	}
	if _, err := q.AssignRole(ctx, sqlc.AssignRoleParams{UserID: member.ID, OrganizationID: i.OrganizationID, RoleID: role.ID, ScopeType: string(authz.ScopeOrg)}); err != nil {
		return err
	}
	return teamAudit(ctx, q, member, i.OrganizationID, "accept_team_invitation", "Joined team: "+role.Name, map[string]any{
		"invitation_id": i.InvitationID, "role_id": strconv.FormatInt(role.ID, 10), "role_name": role.Name,
		"inviter_user_id": inviter.UserID, "inviter_username": inviter.Username, "registered": registered,
	})
}

func teamAudit(ctx context.Context, q sqlc.Querier, actor sqlc.User, orgID int64, event, description string, metadata map[string]any) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fleeterror.NewInternalErrorf("encode team invitation audit metadata: %w", err)
	}
	return q.InsertActivityLog(ctx, sqlc.InsertActivityLogParams{
		EventID: uuid.New(), EventCategory: "auth", EventType: event, Description: description,
		Result: "success", ActorType: "user",
		UserID: sql.NullString{String: actor.UserID, Valid: true}, Username: sql.NullString{String: actor.Username, Valid: true},
		OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
		Metadata:       pqtype.NullRawMessage{RawMessage: encoded, Valid: true}, MemberSiteIds: []int64{},
	})
}
