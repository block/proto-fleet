package hostedaccounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

// Invitation contains public lifecycle metadata, never its stored bearer hash.
type Invitation = sqlc.ListHostedAccountInvitationsRow

type IssuedInvitation struct {
	Invitation Invitation
	Token      string
}

type InvitationPreview struct {
	AccountName string
	Role        Role
	ExpiresAt   time.Time
}

type Registration struct {
	AccountID string
	Cookie    *http.Cookie
}

// Public previews never queue for admission or consume the database pool with
// unbounded concurrent snapshots. The limit is shared across service instances.
var sharedInvitationPreviewSlots = make(chan struct{}, 16)

type InvitationService struct {
	accounts     *Service
	sessions     *session.Service
	admission    *registrationAdmission
	previewSlots chan struct{}
	hashPassword func([]byte) ([]byte, error)
}

func NewInvitationService(accounts *Service, sessions *session.Service) *InvitationService {
	return &InvitationService{accounts: accounts, sessions: sessions, admission: sharedRegistrationAdmission, previewSlots: sharedInvitationPreviewSlots, hashPassword: func(password []byte) ([]byte, error) {
		return bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	}}
}

func (s *InvitationService) Create(ctx context.Context, actorID, orgID int64, accountID string, role Role, lifetime time.Duration) (IssuedInvitation, error) {
	if role.capabilities() == 0 {
		return IssuedInvitation{}, fleeterror.NewInvalidArgumentError("invalid account role")
	}
	if lifetime == 0 {
		lifetime = 24 * time.Hour
	}
	if lifetime <= 0 || lifetime > 7*24*time.Hour {
		return IssuedInvitation{}, fleeterror.NewInvalidArgumentError("invitation lifetime must be at most seven days")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return IssuedInvitation{}, fleeterror.NewInternalError("cannot create invitation")
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	hash, _ := invitationHash(token)
	return db.WithTransaction(ctx, s.accounts.conn, func(q sqlc.Querier) (IssuedInvitation, error) {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return IssuedInvitation{}, err
		}
		path, err := loadPath(ctx, q, orgID, accountID)
		if err != nil {
			return IssuedInvitation{}, err
		}
		_, err = authorize(ctx, q, actorID, orgID, path, ManageMembers)
		if err != nil {
			return IssuedInvitation{}, err
		}
		if err := requireRoleGrant(ctx, q, actorID, orgID, accountID, role); err != nil {
			return IssuedInvitation{}, err
		}
		account := path[len(path)-1]
		if account.Depth == 0 {
			return IssuedInvitation{}, denied()
		}
		fingerprint, err := authorityFingerprint(ctx, q, actorID, orgID, path)
		if err != nil {
			return IssuedInvitation{}, err
		}
		row, err := q.CreateHostedAccountInvitation(ctx, sqlc.CreateHostedAccountInvitationParams{
			InvitationID: uuid.NewString(), OrganizationID: orgID, AccountID: account.ID, InviterID: actorID,
			AuthorityFingerprint: fingerprint, TokenHash: hash, Role: string(role), ExpiresAt: time.Now().Add(lifetime),
		})
		if err != nil {
			return IssuedInvitation{}, err
		}
		if err = audit(ctx, q, actor, account, "create_hosted_account_invitation", map[string]any{"invitation_id": row.InvitationID, "role": role, "expires_at": row.ExpiresAt}); err != nil {
			return IssuedInvitation{}, err
		}
		return IssuedInvitation{Token: token, Invitation: Invitation{InvitationID: row.InvitationID, Role: row.Role, ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt, Available: true}}, nil
	})
}

// List reads a bounded page of unaccepted invitations. Every page
// rechecks current membership authority; the token grants no access itself.
func (s *InvitationService) List(ctx context.Context, actorID, orgID int64, accountID string, pageSize int32, pageToken string) ([]Invitation, string, error) {
	if pageSize < 0 || pageSize > 100 {
		return nil, "", fleeterror.NewInvalidArgumentError("invitation page size must be between 0 and 100")
	}
	if pageSize == 0 {
		pageSize = 25
	}
	cursorTime, cursorID, err := invitationPageCursor(pageToken, orgID, accountID)
	if err != nil {
		return nil, "", err
	}
	path, err := loadPath(ctx, s.accounts.queries, orgID, accountID)
	if err != nil {
		return nil, "", err
	}
	if _, err = authorize(ctx, s.accounts.queries, actorID, orgID, path, ManageMembers); err != nil {
		return nil, "", err
	}
	rows, err := s.accounts.queries.ListHostedAccountInvitations(ctx, sqlc.ListHostedAccountInvitationsParams{
		OrganizationID: orgID, AccountID: path[len(path)-1].ID,
		CursorTime: cursorTime, CursorID: cursorID, PageLimit: pageSize + 1,
	})
	if err != nil {
		return nil, "", err
	}
	if len(rows) <= int(pageSize) {
		return rows, "", nil
	}
	rows = rows[:pageSize]
	last := rows[len(rows)-1]
	return rows, invitationPageToken(orgID, accountID, last), nil
}

func invitationPageToken(orgID int64, accountID string, last Invitation) string {
	payload := fmt.Sprintf("1|%d|%s|%s|%s", orgID, accountID, last.CreatedAt.UTC().Format(time.RFC3339Nano), last.InvitationID)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func invitationPageCursor(token string, orgID int64, accountID string) (sql.NullTime, string, error) {
	if token == "" {
		return sql.NullTime{}, "", nil
	}
	invalid := fleeterror.NewInvalidArgumentError("invalid invitation page token for this account")
	if len(token) > 512 {
		return sql.NullTime{}, "", invalid
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return sql.NullTime{}, "", invalid
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 5 || parts[0] != "1" || parts[1] != strconv.FormatInt(orgID, 10) || parts[2] != accountID {
		return sql.NullTime{}, "", invalid
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[3])
	if err != nil || createdAt.IsZero() {
		return sql.NullTime{}, "", invalid
	}
	id, err := uuid.Parse(parts[4])
	if err != nil || id.String() != parts[4] {
		return sql.NullTime{}, "", invalid
	}
	return sql.NullTime{Time: createdAt, Valid: true}, parts[4], nil
}

func (s *InvitationService) Revoke(ctx context.Context, actorID, orgID int64, accountID, invitationID string) error {
	return db.WithTransactionNoResult(ctx, s.accounts.conn, func(q sqlc.Querier) error {
		actor, err := lockActor(ctx, q, orgID, actorID)
		if err != nil {
			return err
		}
		path, err := loadPath(ctx, q, orgID, accountID)
		if err != nil {
			return err
		}
		if _, err = authorize(ctx, q, actorID, orgID, path, ManageMembers); err != nil {
			return err
		}
		account := path[len(path)-1]
		changed, err := q.RevokeHostedAccountInvitation(ctx, sqlc.RevokeHostedAccountInvitationParams{OrganizationID: orgID, AccountID: account.ID, InvitationID: invitationID})
		if err != nil {
			return err
		}
		if changed != 1 {
			return unavailableInvitation()
		}
		return audit(ctx, q, actor, account, "revoke_hosted_account_invitation", map[string]any{"invitation_id": invitationID})
	})
}

func (s *InvitationService) Preview(ctx context.Context, token string) (InvitationPreview, error) {
	if err := ctx.Err(); err != nil {
		return InvitationPreview{}, fmt.Errorf("invitation preview admission: %w", err)
	}
	select {
	case s.previewSlots <- struct{}{}:
		defer func() { <-s.previewSlots }()
	default:
		return InvitationPreview{}, fleeterror.NewResourceExhaustedErrorf("invitation preview temporarily limited; try again later")
	}
	hash, err := invitationHash(token)
	if err != nil {
		return InvitationPreview{}, err
	}
	// Authority and lifecycle checks share one snapshot without taking the
	// organization or inviter locks reserved for account mutations.
	return db.WithTransaction(ctx, s.accounts.conn, func(q sqlc.Querier) (InvitationPreview, error) {
		i, err := q.GetHostedAccountInvitationByHash(ctx, hash)
		if errors.Is(err, sql.ErrNoRows) {
			return InvitationPreview{}, unavailableInvitation()
		}
		if err != nil {
			return InvitationPreview{}, err
		}
		if _, err := validateInvitation(ctx, q, i); err != nil {
			return InvitationPreview{}, err
		}
		return InvitationPreview{AccountName: i.AccountName, Role: Role(i.Role), ExpiresAt: i.ExpiresAt}, nil
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
}

func (s *InvitationService) Accept(ctx context.Context, token string, userID, orgID int64) (string, error) {
	var accountID string
	err := s.withInvitation(ctx, token, func(q sqlc.Querier, i sqlc.GetHostedAccountInvitationByHashRow, path []Account, inviter sqlc.User) error {
		if i.OrganizationID != orgID {
			return unavailableInvitation()
		}
		if _, err := q.LockActiveHostedAccountUser(ctx, sqlc.LockActiveHostedAccountUserParams{ID: userID, OrganizationID: orgID}); err != nil {
			return accessError(err)
		}
		if err := acceptInvitation(ctx, q, i, path[len(path)-1], inviter, userID); err != nil {
			return err
		}
		accountID = i.ExternalAccountID
		return nil
	})
	return accountID, err
}

// Register creates identity, roleless affiliation, membership and session in the
// same transaction. A rejected or replayed link creates none of these records.
func (s *InvitationService) Register(ctx context.Context, token, username, password, userAgent, ipAddress string) (Registration, error) {
	username = strings.TrimSpace(username)
	if username == "" || !utf8.ValidString(username) || utf8.RuneCountInString(username) > 255 {
		return Registration{}, fleeterror.NewInvalidArgumentError("username must contain 1 to 255 characters")
	}
	if err := auth.ValidatePassword(password); err != nil {
		return Registration{}, fleeterror.NewInvalidArgumentError(err.Error())
	}
	if err := s.admission.checkClient(ctx, ipAddress); err != nil {
		return Registration{}, err
	}
	// Reject unavailable links before password hashing. The consumption
	// transaction below rechecks authority and availability after hashing.
	if _, err := s.Preview(ctx, token); err != nil {
		return Registration{}, err
	}
	// Availability is an optimization; the transaction still checks and the
	// unique constraint remains authoritative for concurrent registrations.
	_, err := s.accounts.queries.GetUserByUsername(ctx, username)
	if err == nil {
		return Registration{}, fleeterror.NewInvalidArgumentError("username unavailable")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Registration{}, err
	}
	tokenHash, err := invitationHash(token)
	if err != nil {
		return Registration{}, err
	}
	release, err := s.admission.acquireHash(ctx, tokenHash)
	if err != nil {
		return Registration{}, err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return Registration{}, fmt.Errorf("register invitation before hashing: %w", err)
	}
	passwordHash, err := s.hashPassword([]byte(password))
	// Bcrypt cannot be interrupted. Keep the slot until it actually returns,
	// then honor cancellation before creating any identity or session.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Registration{}, fmt.Errorf("register invitation after hashing: %w", ctxErr)
	}
	if err != nil {
		return Registration{}, fleeterror.NewInternalError("cannot create login")
	}
	var result Registration
	err = s.withInvitation(ctx, token, func(q sqlc.Querier, i sqlc.GetHostedAccountInvitationByHashRow, path []Account, inviter sqlc.User) error {
		_, err := q.GetUserByUsername(ctx, username)
		if err == nil {
			return fleeterror.NewInvalidArgumentError("username unavailable")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		userID, err := q.CreateUser(ctx, sqlc.CreateUserParams{UserID: uuid.NewString(), Username: username, PasswordHash: string(passwordHash), CreatedAt: time.Now()})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation && pgErr.ConstraintName == "uq_user_username" {
				return fleeterror.NewInvalidArgumentError("username unavailable")
			}
			return err
		}
		if err = q.AddHostedAccountUserAffiliation(ctx, sqlc.AddHostedAccountUserAffiliationParams{UserID: userID, OrganizationID: i.OrganizationID}); err != nil {
			return err
		}
		if err = acceptInvitation(ctx, q, i, path[len(path)-1], inviter, userID); err != nil {
			return err
		}
		created, err := s.sessions.Create(db.WithTxQueries(ctx, q), userID, i.OrganizationID, userAgent, ipAddress)
		if err != nil {
			return err
		}
		result = Registration{AccountID: i.ExternalAccountID, Cookie: s.sessions.CreateCookie(created.SessionID)}
		return nil
	})
	return result, err
}

func acceptInvitation(ctx context.Context, q sqlc.Querier, i sqlc.GetHostedAccountInvitationByHashRow, account Account, inviter sqlc.User, userID int64) error {
	previous, err := q.GetHostedAccountMembership(ctx, sqlc.GetHostedAccountMembershipParams{OrganizationID: i.OrganizationID, AccountID: i.AccountID, UserID: userID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// An invitation to an existing member cannot silently change their role.
	if err == nil && previous.Role != i.Role {
		return fleeterror.NewFailedPreconditionError("user already belongs to this account; manage their role through memberships")
	}
	changed, err := q.ConsumeHostedAccountInvitation(ctx, sqlc.ConsumeHostedAccountInvitationParams{AcceptedUserID: sql.NullInt64{Int64: userID, Valid: true}, InvitationID: i.InvitationID})
	if err != nil {
		return err
	}
	if changed != 1 {
		return unavailableInvitation()
	}
	// Re-establishing a previously revoked membership is explicit acceptance.
	if err = q.SetHostedAccountMembership(ctx, sqlc.SetHostedAccountMembershipParams{OrganizationID: i.OrganizationID, AccountID: i.AccountID, UserID: userID, Role: i.Role}); err != nil {
		return err
	}
	return audit(ctx, q, inviter, account, "accept_hosted_account_invitation", map[string]any{"invitation_id": i.InvitationID, "member_user_id": userID, "role": i.Role})
}

func (s *InvitationService) withInvitation(ctx context.Context, token string, fn func(sqlc.Querier, sqlc.GetHostedAccountInvitationByHashRow, []Account, sqlc.User) error) error {
	hash, err := invitationHash(token)
	if err != nil {
		return err
	}
	initial, err := s.accounts.queries.GetHostedAccountInvitationByHash(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return unavailableInvitation()
	}
	if err != nil {
		return err
	}
	if invitationUnavailable(initial) {
		return unavailableInvitation()
	}
	return db.WithTransactionNoResult(ctx, s.accounts.conn, func(q sqlc.Querier) error {
		// Same organization lock as account policy and membership changes.
		inviter, err := lockActor(ctx, q, initial.OrganizationID, initial.InviterID)
		if err != nil {
			if fleeterror.IsForbiddenError(err) {
				return unavailableInvitation()
			}
			return err
		}
		i, err := q.GetHostedAccountInvitationByHash(ctx, hash)
		if err != nil {
			return err
		}
		path, err := validateInvitation(ctx, q, i)
		if err != nil {
			return err
		}
		return fn(q, i, path, inviter)
	})
}

func invitationUnavailable(i sqlc.GetHostedAccountInvitationByHashRow) bool {
	return i.RevokedAt.Valid || i.AcceptedAt.Valid || !time.Now().Before(i.ExpiresAt)
}

func validateInvitation(ctx context.Context, q sqlc.Querier, i sqlc.GetHostedAccountInvitationByHashRow) ([]Account, error) {
	if invitationUnavailable(i) {
		return nil, unavailableInvitation()
	}
	path, err := loadPath(ctx, q, i.OrganizationID, i.ExternalAccountID)
	if err != nil {
		return nil, err
	}
	_, err = authorize(ctx, q, i.InviterID, i.OrganizationID, path, ManageMembers)
	if err != nil {
		if fleeterror.IsForbiddenError(err) {
			return nil, unavailableInvitation()
		}
		return nil, err
	}
	if err := requireRoleGrant(ctx, q, i.InviterID, i.OrganizationID, i.ExternalAccountID, Role(i.Role)); err != nil {
		if fleeterror.IsForbiddenError(err) {
			return nil, unavailableInvitation()
		}
		return nil, err
	}
	fingerprint, err := authorityFingerprint(ctx, q, i.InviterID, i.OrganizationID, path)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(fingerprint), []byte(i.AuthorityFingerprint)) != 1 {
		return nil, unavailableInvitation()
	}
	return path, nil
}

func invitationHash(token string) (string, error) {
	if len(token) != base64.RawURLEncoding.EncodedLen(32) {
		return "", unavailableInvitation()
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return "", unavailableInvitation()
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:]), nil
}

func unavailableInvitation() error {
	return fleeterror.NewFailedPreconditionError("invitation unavailable")
}
