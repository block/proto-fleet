package hostedaccounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func registrationFixture(t *testing.T) (*InvitationService, *sql.DB, string) {
	t.Helper()
	conn := dbtest.GetTestDB(t)
	var orgID int64
	require.NoError(t, conn.QueryRowContext(t.Context(), "INSERT INTO organization(org_id,name) VALUES($1,'Registration admission') RETURNING id", uuid.NewString()).Scan(&orgID))
	require.NoError(t, authz.Reconcile(t.Context(), conn))
	q := sqlc.New(conn)
	hostID, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: uuid.NewString(), Username: "registration-host", PasswordHash: "fixture", CreatedAt: time.Now()})
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), "INSERT INTO user_organization(user_id, organization_id, role_id) SELECT $1,$2,id FROM role WHERE organization_id=$2 AND builtin_key='FIELD_TECH'", hostID, orgID)
	require.NoError(t, err)
	var roleID int64
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT id FROM role WHERE organization_id=$1 AND builtin_key='SUPER_ADMIN'", orgID).Scan(&roleID))
	_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: hostID, OrganizationID: orgID, RoleID: roleID, ScopeType: "org"})
	require.NoError(t, err)
	var root string
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT account_id FROM hosted_account WHERE organization_id=$1 AND depth=0", orgID).Scan(&root))
	accounts := NewService(conn)
	account, err := accounts.CreateChild(t.Context(), hostID, orgID, root, "Customer", hostID, DefaultCapabilities)
	require.NoError(t, err)
	sessions := session.NewService(session.Config{Duration: time.Hour, IDBytes: 32, CookieName: "fleet_session"}, sqlstores.NewSQLSessionStore(conn))
	svc := NewInvitationService(accounts, sessions)
	svc.admission = newRegistrationAdmission(sharedRegistrationAdmission.limits, time.Now)
	issued, err := svc.Create(t.Context(), hostID, orgID, account.AccountID, RoleViewer, time.Hour)
	require.NoError(t, err)
	return svc, conn, issued.Token
}

func TestRegistrationRejectsOccupiedUsernameBeforeHashing(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	var calls atomic.Int32
	svc.hashPassword = func(password []byte) ([]byte, error) {
		calls.Add(1)
		return bcrypt.GenerateFromPassword(password, bcrypt.MinCost)
	}
	_, err := sqlc.New(conn).CreateUser(t.Context(), sqlc.CreateUserParams{UserID: uuid.NewString(), Username: "occupied", PasswordHash: "fixture", CreatedAt: time.Now()})
	require.NoError(t, err)
	for range 2 {
		_, err = svc.Register(t.Context(), token, "occupied", "password123", "browser", "192.0.2.1")
		require.True(t, fleeterror.IsInvalidArgumentError(err))
	}
	require.Zero(t, calls.Load(), "rejected usernames must not invoke the password hasher")
	_, err = svc.Preview(t.Context(), token)
	require.NoError(t, err, "a rejected registration must not consume the invitation")
}

func TestRegistrationBoundsReusableInvitationBeforeHashing(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	var calls atomic.Int32
	svc.hashPassword = func(password []byte) ([]byte, error) {
		calls.Add(1)
		return bcrypt.GenerateFromPassword(password, bcrypt.MinCost)
	}
	_, err := conn.ExecContext(t.Context(), `CREATE FUNCTION reject_registration() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'fixture registration rollback' USING ERRCODE='40001'; END $$;
 CREATE TRIGGER reject_registration BEFORE INSERT ON "user" FOR EACH ROW EXECUTE FUNCTION reject_registration()`)
	require.NoError(t, err)
	for i := range 4 {
		_, err = svc.Register(t.Context(), token, fmt.Sprintf("available-%d", i), "password123", "browser", "192.0.2.2")
		if i < 3 {
			require.True(t, db.IsRetryablePostgresError(err), "registration rollback retains the original database error")
		} else {
			require.True(t, fleeterror.IsResourceExhaustedError(err), "changing the username cannot evade the invitation's work budget")
		}
	}
	require.Equal(t, int32(3), calls.Load())
	_, err = svc.Preview(t.Context(), token)
	require.NoError(t, err, "throttling must not consume or revoke the invitation")
}

func requireNoRegistration(t *testing.T, conn *sql.DB, username string) {
	t.Helper()
	var users, sessions int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM "user" WHERE username=$1`, username).Scan(&users))
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM session`).Scan(&sessions))
	require.Zero(t, users)
	require.Zero(t, sessions)
}

func TestRegistrationInvalidInvitationsDoNotSpendHashBudget(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	svc.hashPassword = func([]byte) ([]byte, error) { t.Fatal("invalid invitation reached hashing"); return nil, nil }
	for i := range 4 {
		guessed := "AAAA-AAA" + string(rune('A'+i))
		_, err := svc.Register(t.Context(), guessed, "new-user", "password123", "browser", "192.0.2.4")
		require.True(t, fleeterror.IsFailedPreconditionError(err))
	}
	require.Empty(t, svc.admission.invitations)
	require.Zero(t, svc.admission.total.count)
	require.Zero(t, svc.admission.active)
	requireNoRegistration(t, conn, "new-user")
	_, err := svc.Preview(t.Context(), token)
	require.NoError(t, err)
}

func TestRegistrationThrottleExpiresWithoutConsumingInvitation(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	now := time.Now()
	svc.admission = newRegistrationAdmission(sharedRegistrationAdmission.limits, func() time.Time { return now })
	svc.hashPassword = func([]byte) ([]byte, error) { return nil, errors.New("fixture hasher failure") }
	for range 3 {
		_, err := svc.Register(t.Context(), token, "eventual-user", "password123", "", "")
		requireRegistrationInternalError(t, err)
		require.Zero(t, svc.admission.active, "hasher errors release in-flight admission")
	}
	_, err := svc.Register(t.Context(), token, "eventual-user", "password123", "", "")
	require.True(t, fleeterror.IsResourceExhaustedError(err))
	requireNoRegistration(t, conn, "eventual-user")
	_, err = svc.Preview(t.Context(), token)
	require.NoError(t, err)
	now = now.Add(time.Minute)
	svc.hashPassword = func(password []byte) ([]byte, error) { return bcrypt.GenerateFromPassword(password, bcrypt.MinCost) }
	registered, err := svc.Register(t.Context(), token, "eventual-user", "password123", "", "")
	require.NoError(t, err)
	require.NotNil(t, registered.Cookie)
	_, err = svc.Preview(t.Context(), token)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
}

func TestRegistrationCancellationRetainsSlotUntilHasherReturns(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var calls atomic.Int32
	svc.hashPassword = func(password []byte) ([]byte, error) {
		calls.Add(1)
		return bcrypt.GenerateFromPassword(password, bcrypt.MinCost)
	}
	_, err := svc.Register(ctx, token, "canceled-user", "password123", "", "")
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls.Load())
	require.Zero(t, svc.admission.active)
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	entered, finish := make(chan struct{}), make(chan struct{})
	svc.hashPassword = func(password []byte) ([]byte, error) {
		close(entered)
		<-finish
		return bcrypt.GenerateFromPassword(password, bcrypt.MinCost)
	}
	result := make(chan error, 1)
	go func() { _, err := svc.Register(ctx, token, "canceled-user", "password123", "", ""); result <- err }()
	<-entered
	cancel()
	svc.admission.mu.Lock()
	require.Equal(t, 1, svc.admission.active, "cancellation cannot free CPU capacity while bcrypt is running")
	svc.admission.mu.Unlock()
	close(finish)
	require.ErrorIs(t, <-result, context.Canceled)
	require.Zero(t, svc.admission.active)
	requireNoRegistration(t, conn, "canceled-user")
	_, err = svc.Preview(t.Context(), token)
	require.NoError(t, err)
}

func TestRegistrationRechecksInvitationAfterHashing(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	svc.hashPassword = func(password []byte) ([]byte, error) {
		_, err := conn.ExecContext(t.Context(), `UPDATE hosted_account_invitation SET revoked_at=now()`)
		require.NoError(t, err)
		return bcrypt.GenerateFromPassword(password, bcrypt.MinCost)
	}
	_, err := svc.Register(t.Context(), token, "revoked-user", "password123", "", "")
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.Zero(t, svc.admission.active)
	requireNoRegistration(t, conn, "revoked-user")
}

func TestRegistrationSharedAdmissionBoundsConcurrentAndAggregateHashing(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	services := []*InvitationService{NewInvitationService(svc.accounts, svc.sessions), NewInvitationService(svc.accounts, svc.sessions), NewInvitationService(svc.accounts, svc.sessions)}
	for _, service := range services {
		require.Same(t, sharedRegistrationAdmission, service.admission)
	}
	tokens := []string{token}
	var orgID, hostID int64
	var accountID string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT i.organization_id,i.inviter_id,a.account_id FROM hosted_account_invitation i JOIN hosted_account a ON a.id=i.account_id LIMIT 1`).Scan(&orgID, &hostID, &accountID))
	for range 3 {
		issued, err := svc.Create(t.Context(), hostID, orgID, accountID, RoleViewer, time.Hour)
		require.NoError(t, err)
		tokens = append(tokens, issued.Token)
	}
	entered, finish := make(chan struct{}, 2), make(chan struct{})
	var calls atomic.Int32
	for _, service := range services {
		service.hashPassword = func([]byte) ([]byte, error) {
			calls.Add(1)
			entered <- struct{}{}
			<-finish
			return nil, errors.New("fixture failed hash")
		}
	}
	results := make(chan error, 2)
	for i := range 2 {
		go func() {
			_, err := services[i].Register(t.Context(), tokens[i], fmt.Sprintf("parallel-%d", i), "password123", "", "")
			results <- err
		}()
	}
	for range 2 {
		<-entered
	}
	_, err := services[2].Register(t.Context(), tokens[2], "parallel-2", "password123", "", "")
	require.True(t, fleeterror.IsResourceExhaustedError(err))
	require.Equal(t, int32(2), calls.Load())
	close(finish)
	for range 2 {
		requireRegistrationInternalError(t, <-results)
	}
	// Test aggregate sharing with isolated state and a deterministic clock;
	// never override or reset the production default used above.
	now := time.Now()
	limits := sharedRegistrationAdmission.limits
	limits.aggregate = 3
	gate := newRegistrationAdmission(limits, func() time.Time { return now })
	for _, service := range services {
		service.admission = gate
		service.hashPassword = func([]byte) ([]byte, error) { return nil, errors.New("fixture failed hash") }
	}
	for i := range 3 {
		_, err = services[i].Register(t.Context(), tokens[i], fmt.Sprintf("parallel-%d", i), "password123", "", "")
		requireRegistrationInternalError(t, err)
	}
	require.Zero(t, gate.active)
	_, err = services[2].Register(t.Context(), tokens[3], "parallel-3", "password123", "", "")
	require.True(t, fleeterror.IsResourceExhaustedError(err), "aggregate allowance remains exhausted after slots release")
	for i, token := range tokens {
		requireNoRegistration(t, conn, fmt.Sprintf("parallel-%d", i))
		_, err = svc.Preview(t.Context(), token)
		require.NoError(t, err)
	}
}

func requireRegistrationInternalError(t *testing.T, err error) {
	t.Helper()
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	require.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
}

func TestRegistrationUsernameRacePreservesAtomicUniqueness(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	var orgID, hostID int64
	var accountID string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT i.organization_id,i.inviter_id,a.account_id FROM hosted_account_invitation i JOIN hosted_account a ON a.id=i.account_id LIMIT 1`).Scan(&orgID, &hostID, &accountID))
	issued, err := svc.Create(t.Context(), hostID, orgID, accountID, RoleViewer, time.Hour)
	require.NoError(t, err)
	tokens := []string{token, issued.Token}
	entered, finish := make(chan struct{}, 2), make(chan struct{})
	svc.hashPassword = func(password []byte) ([]byte, error) {
		entered <- struct{}{}
		<-finish
		return bcrypt.GenerateFromPassword(password, bcrypt.MinCost)
	}
	type outcome struct {
		token string
		err   error
	}
	results := make(chan outcome, 2)
	for _, token := range tokens {
		go func() {
			_, err := svc.Register(t.Context(), token, "racing-user", "password123", "", "")
			results <- outcome{token, err}
		}()
	}
	for range 2 {
		<-entered
	}
	close(finish)
	var successful int
	for range 2 {
		result := <-results
		if result.err == nil {
			successful++
		} else {
			require.True(t, fleeterror.IsInvalidArgumentError(result.err))
			_, err := svc.Preview(t.Context(), result.token)
			require.NoError(t, err, "losing username race does not consume the invitation")
		}
	}
	require.Equal(t, 1, successful)
	var users, sessions int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM "user" WHERE username='racing-user'`).Scan(&users))
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM session`).Scan(&sessions))
	require.Equal(t, 1, users)
	require.Equal(t, 1, sessions)
	require.Zero(t, svc.admission.active)
}

func TestInvitationPreviewAdmissionRejectsWithoutQueuingAndReleasesSlots(t *testing.T) {
	svc, _, token := registrationFixture(t)
	svc.previewSlots = make(chan struct{}, 1)
	svc.previewSlots <- struct{}{}
	_, err := svc.Preview(t.Context(), token)
	require.True(t, fleeterror.IsResourceExhaustedError(err))
	<-svc.previewSlots
	_, err = svc.Preview(t.Context(), "invalid")
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.Empty(t, svc.previewSlots, "invalid links release preview admission")
	_, err = svc.Preview(t.Context(), token)
	require.NoError(t, err)
	require.Empty(t, svc.previewSlots, "successful previews release admission")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = svc.Preview(ctx, token)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, svc.previewSlots, "cancelled previews retain no admission")
}
