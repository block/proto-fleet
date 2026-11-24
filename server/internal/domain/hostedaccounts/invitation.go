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

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
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

type InvitationService struct {
	invitationCore
}

func NewInvitationService(accounts *Service, sessions *session.Service) *InvitationService {
	return &InvitationService{newInvitationCore(accounts, sessions)}
}

func (s *InvitationService) Create(ctx context.Context, actorID, orgID int64, accountID string, role Role, lifetime time.Duration) (IssuedInvitation, error) {
	if role.capabilities() == 0 {
		return IssuedInvitation{}, fleeterror.NewInvalidArgumentError("invalid account role")
	}
	lifetime, err := invitationLifetime(lifetime)
	if err != nil {
		return IssuedInvitation{}, err
	}
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
		var row sqlc.HostedAccountInvitation
		token, err := s.issueCode(ctx, q, func(hash string) (err error) {
			row, err = q.CreateHostedAccountInvitation(ctx, sqlc.CreateHostedAccountInvitationParams{
				InvitationID: uuid.NewString(), OrganizationID: orgID, AccountID: account.ID, InviterID: actorID,
				AuthorityFingerprint: fingerprint, TokenHash: hash, Role: string(role), ExpiresAt: time.Now().Add(lifetime),
			})
			return err
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
	release, err := s.admitPreview(ctx)
	if err != nil {
		return InvitationPreview{}, err
	}
	defer release()
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
	login, release, err := s.prepareRegistration(ctx, token, username, password, ipAddress, func(ctx context.Context, token string) error {
		_, err := s.Preview(ctx, token)
		return err
	})
	if err != nil {
		return Registration{}, err
	}
	defer release()
	var result Registration
	err = s.withInvitation(ctx, token, func(q sqlc.Querier, i sqlc.GetHostedAccountInvitationByHashRow, path []Account, inviter sqlc.User) error {
		userID, err := createInvitedLogin(ctx, q, login)
		if err != nil {
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

// 32 symbols give eight independently uniform characters 40 bits of entropy.
// That margin relies on the deployment-wide lookup budget in CheckClientRateLimit.
const invitationAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

const invitationCodeLength = 8

func newInvitationCode() (string, error) {
	var secret [invitationCodeLength]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", fmt.Errorf("generate invitation code: %w", err)
	}
	for i := range secret {
		// 32 divides 256, so masking does not introduce modulo bias.
		secret[i] = invitationAlphabet[secret[i]&31]
	}
	return string(secret[:4]) + "-" + string(secret[4:]), nil
}

func invitationHash(token string) (string, error) {
	if len(token) > 128 {
		return "", unavailableInvitation()
	}
	// Only ASCII spaces and ASCII case folding are permitted. Do not silently
	// alias ambiguous symbols or Unicode lookalikes to valid code characters.
	token = strings.ReplaceAll(token, " ", "")
	groups := strings.Split(token, "-")
	for _, group := range groups {
		if len(group) == 0 || len(group)%4 != 0 {
			return "", unavailableInvitation()
		}
	}
	token = strings.Join(groups, "")
	if len(token) != invitationCodeLength {
		return "", unavailableInvitation()
	}
	var normalized [invitationCodeLength]byte
	for i := range token {
		char := token[i]
		if char >= 'a' && char <= 'z' {
			char -= 'a' - 'A'
		}
		if !strings.ContainsRune(invitationAlphabet, rune(char)) {
			return "", unavailableInvitation()
		}
		normalized[i] = char
	}
	sum := sha256.Sum256(normalized[:])
	return hex.EncodeToString(sum[:]), nil
}

func unavailableInvitation() error {
	return fleeterror.NewFailedPreconditionError("invitation unavailable")
}
