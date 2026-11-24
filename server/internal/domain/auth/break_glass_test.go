package auth

import (
	"context"
	"strings"
	"testing"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/require"
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

func (s *breakGlassStoreStub) AdminResetUserPassword(_ context.Context, userID int64, passwordHash string) (int64, error) {
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

func TestBreakGlassResetSuperAdminPasswordRejectsInvalidPasswordBeforeTransaction(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  string
	}{
		{name: "too short", password: "short", wantErr: "at least 8 characters"},
		{name: "too many bytes", password: strings.Repeat("a", 73), wantErr: "at most 72 bytes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := &breakGlassTransactorStub{}
			service := NewBreakGlassService(&breakGlassStoreStub{}, tx, &sessionRevokerStub{}, &activityLoggerStub{})

			_, err := service.ResetSuperAdminPassword(context.Background(), test.password)

			require.ErrorContains(t, err, test.wantErr)
			require.False(t, tx.called)
		})
	}
}
