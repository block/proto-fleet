package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateKeepsBestEffortActivityUpdateForUnclassifiedError(t *testing.T) {
	t.Parallel()

	originalLastActivity := time.Now().Add(-time.Minute)
	originalExpiry := time.Now().Add(time.Hour)
	store := &validateSessionStore{
		session: &Session{
			SessionID:      "0123456789abcdef",
			UserID:         1,
			OrganizationID: 2,
			LastActivity:   originalLastActivity,
			ExpiresAt:      originalExpiry,
		},
		updateErr: errors.New("activity update failed"),
	}
	svc := NewServiceWithValidationFailureClassifier(Config{Duration: 2 * time.Hour}, store, func(error) bool {
		return false
	})

	got, err := svc.Validate(context.Background(), store.session.SessionID)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1, store.updateCalls)
	assert.True(t, got.LastActivity.After(originalLastActivity))
	assert.True(t, got.ExpiresAt.After(originalExpiry))
}

func TestValidateFailsClosedWhenActivityUpdateErrorIsClassified(t *testing.T) {
	t.Parallel()

	failoverErr := errors.New("database failover")
	store := &validateSessionStore{
		session: &Session{
			SessionID:      "0123456789abcdef",
			UserID:         1,
			OrganizationID: 2,
			LastActivity:   time.Now().Add(-time.Minute),
			ExpiresAt:      time.Now().Add(time.Hour),
		},
		updateErr: failoverErr,
	}
	svc := NewServiceWithValidationFailureClassifier(Config{Duration: 2 * time.Hour}, store, func(err error) bool {
		return errors.Is(err, failoverErr)
	})

	got, err := svc.Validate(context.Background(), store.session.SessionID)

	require.Error(t, err)
	assert.Nil(t, got)
	assert.Equal(t, 1, store.updateCalls)
	assertSanitizedSessionUnavailable(t, err, failoverErr)
}

func TestValidateFailsClosedWhenSessionLookupErrorIsClassified(t *testing.T) {
	t.Parallel()

	failoverErr := errors.New("database failover")
	store := &validateSessionStore{getErr: failoverErr}
	svc := NewServiceWithValidationFailureClassifier(Config{Duration: 2 * time.Hour}, store, func(err error) bool {
		return errors.Is(err, failoverErr)
	})

	got, err := svc.Validate(context.Background(), "0123456789abcdef")

	require.Error(t, err)
	assert.Nil(t, got)
	assert.Equal(t, 0, store.updateCalls)
	assertSanitizedSessionUnavailable(t, err, failoverErr)
}

func assertSanitizedSessionUnavailable(t *testing.T, err error, rawCause error) {
	t.Helper()

	assert.True(t, fleeterror.IsUnavailableError(err))
	assert.False(t, errors.Is(err, rawCause))

	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, sessionValidationUnavailableMessage, fleetErr.DebugMessage)
	assert.Equal(t, sessionValidationUnavailableMessage, fleetErr.ConnectError().Message())
	assert.NotContains(t, fleetErr.ConnectError().Message(), rawCause.Error())
}

type validateSessionStore struct {
	session     *Session
	getErr      error
	updateErr   error
	updateCalls int
}

func (s *validateSessionStore) CreateSession(context.Context, *Session) error {
	return nil
}

func (s *validateSessionStore) GetSessionByID(context.Context, string) (*Session, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.session, nil
}

func (s *validateSessionStore) UpdateSessionActivity(context.Context, string, time.Time, time.Time) error {
	s.updateCalls++
	return s.updateErr
}

func (s *validateSessionStore) RevokeSession(context.Context, string, time.Time) error {
	return nil
}

func (s *validateSessionStore) RevokeAllSessionsByUserID(context.Context, int64, time.Time) error {
	return nil
}

func (s *validateSessionStore) DeleteExpiredSessions(context.Context, time.Time) (int64, error) {
	return 0, nil
}
