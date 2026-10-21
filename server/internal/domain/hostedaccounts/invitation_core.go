package hostedaccounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// Public previews never queue for a process-local slot or consume the database
// pool with unbounded concurrent snapshots. Public handlers also apply the
// PostgreSQL client rate limit across all processes.
var sharedInvitationPreviewSlots = make(chan struct{}, 16)

// invitationCore holds the code issuance, public admission and invited-login
// mechanics shared by customer account and team invitations, which draw from
// one code namespace and one redemption endpoint.
type invitationCore struct {
	accounts     *Service
	sessions     *session.Service
	admission    *registrationAdmission
	previewSlots chan struct{}
	hashPassword func([]byte) ([]byte, error)
	generateCode func() (string, error)
}

func newInvitationCore(accounts *Service, sessions *session.Service) invitationCore {
	return invitationCore{accounts: accounts, sessions: sessions, admission: sharedRegistrationAdmission, previewSlots: sharedInvitationPreviewSlots, generateCode: newInvitationCode,
		hashPassword: func(password []byte) ([]byte, error) {
			return bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
		}}
}

func invitationLifetime(lifetime time.Duration) (time.Duration, error) {
	if lifetime == 0 {
		return 24 * time.Hour, nil
	}
	if lifetime < 0 || lifetime > 24*time.Hour {
		return 0, fleeterror.NewInvalidArgumentError("invitation lifetime must be at most 24 hours")
	}
	return lifetime, nil
}

// issueCode stores a fresh code unused by either invitation table. insert
// returns sql.ErrNoRows when ON CONFLICT DO NOTHING skipped its row, which
// leaves the transaction usable for another attempt.
func (c *invitationCore) issueCode(ctx context.Context, q sqlc.Querier, insert func(hash string) error) (string, error) {
	for range 5 {
		token, err := c.generateCode()
		if err != nil {
			return "", fleeterror.NewInternalError("cannot create invitation")
		}
		hash, err := invitationHash(token)
		if err != nil {
			return "", err
		}
		inUse, err := q.InvitationTokenHashInUse(ctx, hash)
		if err != nil {
			return "", err
		}
		if inUse {
			continue
		}
		err = insert(hash)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", err
		}
		return token, nil
	}
	return "", fleeterror.NewInternalError("cannot create invitation")
}

// admitPreview claims a preview slot without waiting; release returns it.
func (c *invitationCore) admitPreview(ctx context.Context) (release func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("invitation preview admission: %w", err)
	}
	select {
	case c.previewSlots <- struct{}{}:
		return func() { <-c.previewSlots }, nil
	default:
		return nil, fleeterror.NewResourceExhaustedErrorf("invitation preview temporarily limited; try again later")
	}
}

type invitedLogin struct {
	username     string
	passwordHash []byte
}

// prepareRegistration validates and hashes new credentials before the
// redemption transaction. Unavailable codes are rejected before bcrypt; the
// transaction rechecks availability. Callers hold release until they finish.
func (c *invitationCore) prepareRegistration(ctx context.Context, token, username, password, ipAddress string, preview func(context.Context, string) error) (invitedLogin, func(), error) {
	username = strings.TrimSpace(username)
	if username == "" || !utf8.ValidString(username) || utf8.RuneCountInString(username) > 255 {
		return invitedLogin{}, nil, fleeterror.NewInvalidArgumentError("username must contain 1 to 255 characters")
	}
	if err := auth.ValidatePassword(password); err != nil {
		return invitedLogin{}, nil, fleeterror.NewInvalidArgumentError(err.Error())
	}
	if err := c.admission.checkClient(ctx, ipAddress); err != nil {
		return invitedLogin{}, nil, err
	}
	if err := preview(ctx, token); err != nil {
		return invitedLogin{}, nil, err
	}
	// Availability is an optimization; the transaction still checks and the
	// unique constraint remains authoritative for concurrent registrations.
	if _, err := c.accounts.queries.GetUserByUsername(ctx, username); err == nil {
		return invitedLogin{}, nil, fleeterror.NewInvalidArgumentError("username unavailable")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return invitedLogin{}, nil, err
	}
	tokenHash, err := invitationHash(token)
	if err != nil {
		return invitedLogin{}, nil, err
	}
	release, err := c.admission.acquireHash(ctx, tokenHash)
	if err != nil {
		return invitedLogin{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		release()
		return invitedLogin{}, nil, fmt.Errorf("register invitation before hashing: %w", err)
	}
	passwordHash, err := c.hashPassword([]byte(password))
	// Bcrypt cannot be interrupted. Keep the slot until it actually returns,
	// then honor cancellation before creating any identity or session.
	if ctxErr := ctx.Err(); ctxErr != nil {
		release()
		return invitedLogin{}, nil, fmt.Errorf("register invitation after hashing: %w", ctxErr)
	}
	if err != nil {
		release()
		return invitedLogin{}, nil, fleeterror.NewInternalError("cannot create login")
	}
	return invitedLogin{username: username, passwordHash: passwordHash}, release, nil
}

// createInvitedLogin inserts the prepared identity inside the redemption
// transaction, mapping a concurrent username claim to the same public error.
func createInvitedLogin(ctx context.Context, q sqlc.Querier, login invitedLogin) (int64, error) {
	if _, err := q.GetUserByUsername(ctx, login.username); err == nil {
		return 0, fleeterror.NewInvalidArgumentError("username unavailable")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	userID, err := q.CreateUser(ctx, sqlc.CreateUserParams{UserID: uuid.NewString(), Username: login.username, PasswordHash: string(login.passwordHash), CreatedAt: time.Now()})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation && pgErr.ConstraintName == "uq_user_username" {
			return 0, fleeterror.NewInvalidArgumentError("username unavailable")
		}
		return 0, err
	}
	return userID, nil
}
