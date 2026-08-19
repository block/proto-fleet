package auth

import (
	"context"
	"errors"
	"testing"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type breakGlassStoreStub struct {
	admins       []stores.BreakGlassSuperAdmin
	hasUser      bool
	lockErr      error
	hasUserErr   error
	updateErr    error
	rowsAffected int64
	updatedID    int64
	passwordHash string
}

func (s *breakGlassStoreStub) HasUser(context.Context) (bool, error) {
	return s.hasUser, s.hasUserErr
}

func (s *breakGlassStoreStub) LockActiveSuperAdminUsers(context.Context) ([]stores.BreakGlassSuperAdmin, error) {
	return s.admins, s.lockErr
}

func (s *breakGlassStoreStub) BreakGlassResetUserPassword(_ context.Context, userID int64, passwordHash string) (int64, error) {
	s.updatedID = userID
	s.passwordHash = passwordHash
	return s.rowsAffected, s.updateErr
}

type breakGlassTransactorStub struct {
	called    bool
	committed bool
}

func (s *breakGlassTransactorStub) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	s.called = true
	if err := fn(ctx); err != nil {
		return err
	}
	s.committed = true
	return nil
}

func (s *breakGlassTransactorStub) RunInTxWithResult(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	return fn(ctx)
}

type sessionRevokerStub struct {
	userID int64
	err    error
}

func (s *sessionRevokerStub) RevokeAllSessions(_ context.Context, userID int64) error {
	s.userID = userID
	return s.err
}

type activityLoggerStub struct {
	event activitymodels.Event
	err   error
}

func (s *activityLoggerStub) LogStrict(_ context.Context, event activitymodels.Event) error {
	s.event = event
	return s.err
}

func TestBreakGlassResetSuperAdminPassword(t *testing.T) {
	admin := stores.BreakGlassSuperAdmin{ID: 41, ExternalUserID: "user-external", Username: "owner", OrganizationID: 17}
	store := &breakGlassStoreStub{admins: []stores.BreakGlassSuperAdmin{admin}, hasUser: true, rowsAffected: 1}
	tx := &breakGlassTransactorStub{}
	sessions := &sessionRevokerStub{}
	activityLog := &activityLoggerStub{}
	service := NewBreakGlassService(store, tx, sessions, activityLog)

	result, err := service.ResetSuperAdminPassword(context.Background(), "new-password")

	require.NoError(t, err)
	require.Equal(t, "owner", result.Username)
	require.True(t, tx.committed)
	require.Equal(t, admin.ID, store.updatedID)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(store.passwordHash), []byte("new-password")))
	require.Equal(t, admin.ID, sessions.userID)
	require.Equal(t, breakGlassResetEventType, activityLog.event.Type)
	require.Equal(t, activitymodels.ActorSystem, activityLog.event.ActorType)
	require.Nil(t, activityLog.event.UserID)
	require.Nil(t, activityLog.event.Username)
	require.Equal(t, admin.OrganizationID, *activityLog.event.OrganizationID)
	require.Equal(t, admin.ExternalUserID, activityLog.event.Metadata["target_user_id"])
	require.Equal(t, admin.Username, activityLog.event.Metadata["target_username"])
}

func TestBreakGlassResetSuperAdminPasswordRejectsInvalidCardinality(t *testing.T) {
	admin := stores.BreakGlassSuperAdmin{ID: 1}
	tests := []struct {
		name    string
		admins  []stores.BreakGlassSuperAdmin
		hasUser bool
		wantErr string
	}{
		{name: "onboarding incomplete", wantErr: "complete onboarding"},
		{name: "missing super admin", hasUser: true, wantErr: "no live org-scope SUPER_ADMIN"},
		{name: "multiple super admins", admins: []stores.BreakGlassSuperAdmin{admin, admin}, hasUser: true, wantErr: "found 2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &breakGlassStoreStub{admins: test.admins, hasUser: test.hasUser, rowsAffected: 1}
			tx := &breakGlassTransactorStub{}
			service := NewBreakGlassService(store, tx, &sessionRevokerStub{}, &activityLoggerStub{})

			_, err := service.ResetSuperAdminPassword(context.Background(), "new-password")

			require.ErrorContains(t, err, test.wantErr)
			require.False(t, tx.committed)
			require.Empty(t, store.passwordHash)
		})
	}
}

func TestBreakGlassResetSuperAdminPasswordRequiresExactlyOneUpdate(t *testing.T) {
	store := &breakGlassStoreStub{
		admins: []stores.BreakGlassSuperAdmin{{ID: 1}}, hasUser: true, rowsAffected: 0,
	}
	tx := &breakGlassTransactorStub{}
	service := NewBreakGlassService(store, tx, &sessionRevokerStub{}, &activityLoggerStub{})

	_, err := service.ResetSuperAdminPassword(context.Background(), "new-password")

	require.ErrorContains(t, err, "expected one updated user")
	require.False(t, tx.committed)
}

func TestBreakGlassResetSuperAdminPasswordRollsBackWhenAuditFails(t *testing.T) {
	store := &breakGlassStoreStub{
		admins: []stores.BreakGlassSuperAdmin{{ID: 1}}, hasUser: true, rowsAffected: 1,
	}
	tx := &breakGlassTransactorStub{}
	service := NewBreakGlassService(store, tx, &sessionRevokerStub{}, &activityLoggerStub{err: errors.New("insert failed")})

	_, err := service.ResetSuperAdminPassword(context.Background(), "new-password")

	require.ErrorContains(t, err, "write password reset activity")
	require.False(t, tx.committed)
}

func TestBreakGlassResetSuperAdminPasswordHashesBeforeTransaction(t *testing.T) {
	tx := &breakGlassTransactorStub{}
	service := NewBreakGlassService(&breakGlassStoreStub{}, tx, &sessionRevokerStub{}, &activityLoggerStub{})

	_, err := service.ResetSuperAdminPassword(context.Background(), string(make([]byte, 73)))

	require.ErrorContains(t, err, "hash password")
	require.False(t, tx.called)
}
