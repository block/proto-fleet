package auth_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	domainauth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type seededSuperAdmin struct {
	ID             int64
	ExternalUserID string
	Username       string
	OrganizationID int64
	PasswordHash   string
}

func seedSuperAdmin(t *testing.T, db *sql.DB, suffix string) seededSuperAdmin {
	t.Helper()
	ctx := t.Context()
	queries := sqlc.New(db)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("old-password-"+suffix), bcrypt.MinCost)
	require.NoError(t, err)

	userID, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		UserID:       "external-" + suffix,
		Username:     "owner-" + suffix,
		PasswordHash: string(oldHash),
		CreatedAt:    time.Now(),
	})
	require.NoError(t, err)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name:  "Organization " + suffix,
		OrgID: "organization-" + suffix,
	})
	require.NoError(t, err)
	roles, err := authz.SeedOrgBuiltins(ctx, queries, orgID)
	require.NoError(t, err)
	roleID := roles[authz.BuiltinKeySuperAdmin]
	require.NotZero(t, roleID)
	require.NoError(t, queries.CreateUserOrganization(ctx, sqlc.CreateUserOrganizationParams{
		UserID: userID, OrganizationID: orgID, RoleID: roleID,
	}))
	_, err = queries.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID: userID, OrganizationID: orgID, RoleID: roleID,
		ScopeType: "org", ScopeID: sql.NullInt64{},
	})
	require.NoError(t, err)

	return seededSuperAdmin{
		ID: userID, ExternalUserID: "external-" + suffix, Username: "owner-" + suffix,
		OrganizationID: orgID, PasswordHash: string(oldHash),
	}
}

func newBreakGlassService(db *sql.DB, logger interface {
	LogStrict(ctx context.Context, event activitymodels.Event) error
}) *domainauth.BreakGlassService {
	return domainauth.NewBreakGlassService(
		sqlstores.NewSQLUserStore(db),
		sqlstores.NewSQLTransactor(db),
		session.NewService(session.Config{}, sqlstores.NewSQLSessionStore(db)),
		logger,
	)
}

func TestBreakGlassResetIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	admin := seedSuperAdmin(t, db, "success")
	ctx := t.Context()
	now := time.Now()
	sessionStore := sqlstores.NewSQLSessionStore(db)
	require.NoError(t, sessionStore.CreateSession(ctx, &session.Session{
		SessionID: "break-glass-session", UserID: admin.ID, OrganizationID: admin.OrganizationID,
		CreatedAt: now, LastActivity: now, ExpiresAt: now.Add(time.Hour),
	}))
	service := newBreakGlassService(db, activity.NewService(sqlstores.NewSQLActivityStore(db)))

	result, err := service.ResetSuperAdminPassword(ctx, "new-break-glass-password")

	require.NoError(t, err)
	require.Equal(t, admin.Username, result.Username)
	var passwordHash string
	var requiresChange bool
	var passwordUpdatedAt sql.NullTime
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT password_hash, requires_password_change, password_updated_at
		FROM "user" WHERE id = $1`, admin.ID).Scan(&passwordHash, &requiresChange, &passwordUpdatedAt))
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("new-break-glass-password")))
	require.Error(t, bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("old-password-success")))
	require.True(t, requiresChange)
	require.True(t, passwordUpdatedAt.Valid)

	var revoked bool
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT revoked_at IS NOT NULL FROM session WHERE session_id = $1`,
		"break-glass-session").Scan(&revoked))
	require.True(t, revoked)

	var actorType string
	var actorUserID, actorUsername sql.NullString
	var orgID int64
	var rawMetadata []byte
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT actor_type, user_id, username, organization_id, metadata
		FROM activity_log WHERE event_type = 'cli_reset_password'`).Scan(
		&actorType, &actorUserID, &actorUsername, &orgID, &rawMetadata))
	require.Equal(t, "system", actorType)
	require.False(t, actorUserID.Valid)
	require.False(t, actorUsername.Valid)
	require.Equal(t, admin.OrganizationID, orgID)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(rawMetadata, &metadata))
	require.Equal(t, admin.ExternalUserID, metadata["target_user_id"])
	require.Equal(t, admin.Username, metadata["target_username"])

	var displayLabel string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT activity_display_label('cli_reset_password', CAST(NULL AS TEXT), NULL, $1::jsonb, '')`,
		string(rawMetadata)).Scan(&displayLabel))
	require.Equal(t, "Break-glass password reset for "+admin.Username, displayLabel)
}

type failingActivityLogger struct{}

func (failingActivityLogger) LogStrict(context.Context, activitymodels.Event) error {
	return errors.New("forced audit failure")
}

func TestBreakGlassResetRollsBackDatabaseChangesWhenAuditFails(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	admin := seedSuperAdmin(t, db, "rollback")
	ctx := t.Context()
	now := time.Now()
	require.NoError(t, sqlstores.NewSQLSessionStore(db).CreateSession(ctx, &session.Session{
		SessionID: "rollback-session", UserID: admin.ID, OrganizationID: admin.OrganizationID,
		CreatedAt: now, LastActivity: now, ExpiresAt: now.Add(time.Hour),
	}))
	service := newBreakGlassService(db, failingActivityLogger{})

	_, err := service.ResetSuperAdminPassword(ctx, "password-that-must-roll-back")

	require.ErrorContains(t, err, "write password reset activity")
	var passwordHash string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT password_hash FROM "user" WHERE id = $1`, admin.ID).Scan(&passwordHash))
	require.Equal(t, admin.PasswordHash, passwordHash)
	var revoked bool
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT revoked_at IS NOT NULL FROM session WHERE session_id = $1`,
		"rollback-session").Scan(&revoked))
	require.False(t, revoked)
}

func TestBreakGlassResetRejectsInvalidDatabaseCardinality(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	service := newBreakGlassService(db, activity.NewService(sqlstores.NewSQLActivityStore(db)))

	_, err := service.ResetSuperAdminPassword(ctx, "unused-password")
	require.ErrorContains(t, err, "complete onboarding")

	deletedAdmin := seedSuperAdmin(t, db, "deleted")
	_, err = db.ExecContext(ctx, `UPDATE "user" SET deleted_at = NOW() WHERE id = $1`, deletedAdmin.ID)
	require.NoError(t, err)
	_, err = service.ResetSuperAdminPassword(ctx, "unused-password")
	require.ErrorContains(t, err, "no live org-scope SUPER_ADMIN")

	firstAdmin := seedSuperAdmin(t, db, "multiple-one")
	secondAdmin := seedSuperAdmin(t, db, "multiple-two")
	_, err = service.ResetSuperAdminPassword(ctx, "unused-password")
	require.ErrorContains(t, err, "found 2")
	for _, admin := range []seededSuperAdmin{firstAdmin, secondAdmin} {
		var passwordHash string
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT password_hash FROM "user" WHERE id = $1`, admin.ID).Scan(&passwordHash))
		require.Equal(t, admin.PasswordHash, passwordHash)
	}
	var eventCount int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM activity_log WHERE event_type = 'cli_reset_password'`).Scan(&eventCount))
	require.Zero(t, eventCount)
}

func TestBreakGlassConcurrentResetsSerialize(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	admin := seedSuperAdmin(t, db, "concurrent")
	logger := activity.NewService(sqlstores.NewSQLActivityStore(db))
	services := []*domainauth.BreakGlassService{
		newBreakGlassService(db, logger),
		newBreakGlassService(db, logger),
	}
	passwords := []string{"concurrent-password-one", "concurrent-password-two"}
	errs := make([]error, len(services))
	var wg sync.WaitGroup
	start := make(chan struct{})
	ctx := t.Context()
	for i := range services {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = services[i].ResetSuperAdminPassword(ctx, passwords[i])
		}(i)
	}
	close(start)
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	var passwordHash string
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT password_hash FROM "user" WHERE id = $1`, admin.ID).Scan(&passwordHash))
	matches := 0
	for _, password := range passwords {
		if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil {
			matches++
		}
	}
	require.Equal(t, 1, matches)
	var eventCount int
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM activity_log WHERE event_type = 'cli_reset_password'`).Scan(&eventCount))
	require.Equal(t, 2, eventCount)
}
