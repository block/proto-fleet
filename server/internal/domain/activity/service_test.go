package activity

import (
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const testOrgID = int64(1)

func newTestService(t *testing.T) (*Service, *mocks.MockActivityStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockActivityStore(ctrl)
	svc := NewService(mockStore)
	return svc, mockStore
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
func int64Ptr(i int64) *int64 { return &i }

func TestService_Log_Success(t *testing.T) {
	svc, mockStore := newTestService(t)

	event := models.Event{
		Category:       models.CategoryDeviceCommand,
		Type:           "reboot",
		Description:    "Reboot",
		Result:         models.ResultSuccess,
		ScopeCount:     intPtr(24),
		ActorType:      models.ActorUser,
		UserID:         strPtr("usr_abc"),
		Username:       strPtr("admin"),
		OrganizationID: int64Ptr(testOrgID),
	}

	mockStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ interface{}, got *models.Event) error {
			assert.Equal(t, models.CategoryDeviceCommand, got.Category)
			assert.Equal(t, "reboot", got.Type)
			assert.Equal(t, models.ResultSuccess, got.Result)
			assert.Equal(t, "usr_abc", *got.UserID)
			return nil
		},
	)

	svc.Log(t.Context(), event)
}

func TestService_Log_DefaultsResultToSuccess(t *testing.T) {
	svc, mockStore := newTestService(t)

	event := models.Event{
		Category:       models.CategoryAuth,
		Type:           "login",
		Description:    "Login",
		ActorType:      models.ActorUser,
		OrganizationID: int64Ptr(testOrgID),
	}

	mockStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ interface{}, got *models.Event) error {
			assert.Equal(t, models.ResultSuccess, got.Result)
			return nil
		},
	)

	svc.Log(t.Context(), event)
}

func TestService_Log_DefaultsActorTypeToUser(t *testing.T) {
	svc, mockStore := newTestService(t)

	event := models.Event{
		Category:       models.CategoryAuth,
		Type:           "login",
		Description:    "Login",
		OrganizationID: int64Ptr(testOrgID),
	}

	mockStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ interface{}, got *models.Event) error {
			assert.Equal(t, models.ActorUser, got.ActorType)
			return nil
		},
	)

	svc.Log(t.Context(), event)
}

func TestService_Log_WithResultAndError(t *testing.T) {
	svc, mockStore := newTestService(t)

	event := models.Event{
		Category:     models.CategoryAuth,
		Type:         "login_failed",
		Description:  "Login failed, invalid password",
		Result:       models.ResultFailure,
		ErrorMessage: strPtr("invalid credentials"),
		ActorType:    models.ActorUser,
		Username:     strPtr("unknown_user"),
	}

	mockStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ interface{}, got *models.Event) error {
			assert.Equal(t, models.ResultFailure, got.Result)
			require.NotNil(t, got.ErrorMessage)
			assert.Equal(t, "invalid credentials", *got.ErrorMessage)
			assert.Nil(t, got.OrganizationID)
			return nil
		},
	)

	svc.Log(t.Context(), event)
}

func TestService_Log_ErrorSwallowed(t *testing.T) {
	svc, mockStore := newTestService(t)

	event := models.Event{
		Category:       models.CategoryDeviceCommand,
		Type:           "reboot",
		Description:    "Reboot",
		Result:         models.ResultSuccess,
		ActorType:      models.ActorUser,
		OrganizationID: int64Ptr(testOrgID),
	}

	mockStore.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(errors.New("db connection failed"))

	svc.Log(t.Context(), event)
}

func TestService_Log_WarnsOnMissingOrgIDForNonAuth(t *testing.T) {
	svc, mockStore := newTestService(t)

	event := models.Event{
		Category:    models.CategoryDeviceCommand,
		Type:        "reboot",
		Description: "Reboot",
		Result:      models.ResultSuccess,
		ActorType:   models.ActorUser,
	}

	mockStore.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(nil)

	svc.Log(t.Context(), event)
}

func TestService_Log_WithMetadata(t *testing.T) {
	svc, mockStore := newTestService(t)

	meta := map[string]any{
		"collection_id": "col_123",
		"device_count":  5,
		"schedule_id":   "sched_456",
	}
	event := models.Event{
		Category:       models.CategoryCollection,
		Type:           "add_devices",
		Description:    "Added devices to collection",
		Result:         models.ResultSuccess,
		ActorType:      models.ActorScheduler,
		UserID:         strPtr("usr_abc"),
		Username:       strPtr("admin"),
		OrganizationID: int64Ptr(testOrgID),
		Metadata:       meta,
	}

	mockStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ interface{}, got *models.Event) error {
			require.NotNil(t, got.Metadata)
			assert.Len(t, got.Metadata, 3)
			assert.Equal(t, "col_123", got.Metadata["collection_id"])
			assert.Equal(t, 5, got.Metadata["device_count"])
			assert.Equal(t, "sched_456", got.Metadata["schedule_id"])
			return nil
		},
	)

	svc.Log(t.Context(), event)
}

func TestService_Log_NilMetadata(t *testing.T) {
	svc, mockStore := newTestService(t)

	event := models.Event{
		Category:       models.CategoryAuth,
		Type:           "login",
		Description:    "Login",
		Result:         models.ResultSuccess,
		ActorType:      models.ActorUser,
		OrganizationID: int64Ptr(testOrgID),
	}

	mockStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ interface{}, got *models.Event) error {
			assert.Nil(t, got.Metadata)
			return nil
		},
	)

	svc.Log(t.Context(), event)
}

func TestService_Log_InvalidCategory(t *testing.T) {
	svc, mockStore := newTestService(t)

	event := models.Event{
		Category:       models.EventCategory("nonexistent"),
		Type:           "test",
		Description:    "Test",
		ActorType:      models.ActorUser,
		OrganizationID: int64Ptr(testOrgID),
	}

	mockStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ interface{}, got *models.Event) error {
			assert.Equal(t, models.EventCategory("nonexistent"), got.Category)
			assert.Equal(t, models.ResultSuccess, got.Result)
			return nil
		},
	)

	svc.Log(t.Context(), event)
}

func TestService_Log_WarnsOnUserIDWithoutUsername(t *testing.T) {
	svc, mockStore := newTestService(t)

	event := models.Event{
		Category:       models.CategoryDeviceCommand,
		Type:           "reboot",
		Description:    "Reboot",
		ActorType:      models.ActorUser,
		UserID:         strPtr("usr_abc"),
		OrganizationID: int64Ptr(testOrgID),
	}

	mockStore.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(nil)

	svc.Log(t.Context(), event)
}

func TestService_List(t *testing.T) {
	svc, mockStore := newTestService(t)

	now := time.Now()
	expected := []models.Entry{
		{
			ID:          1,
			EventID:     "evt-1",
			Category:    "auth",
			Type:        "login",
			Description: "Login",
			Result:      "success",
			ActorType:   "user",
			UserID:      strPtr("usr_abc"),
			Username:    strPtr("admin"),
			CreatedAt:   now,
		},
	}

	filter := models.Filter{OrganizationID: testOrgID, PageSize: 50}
	mockStore.EXPECT().List(gomock.Any(), filter).Return(expected, nil)

	result, err := svc.List(t.Context(), filter)
	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestService_Count(t *testing.T) {
	svc, mockStore := newTestService(t)

	filter := models.Filter{OrganizationID: testOrgID}
	mockStore.EXPECT().Count(gomock.Any(), filter).Return(int64(42), nil)

	count, err := svc.Count(t.Context(), filter)
	require.NoError(t, err)
	assert.Equal(t, int64(42), count)
}

func TestService_GetFilterOptions(t *testing.T) {
	svc, mockStore := newTestService(t)

	eventTypes := []models.EventTypeInfo{
		{EventType: "login", EventCategory: "auth"},
		{EventType: "reboot", EventCategory: "device_command"},
	}
	scopeTypes := []string{"group", "rack"}
	users := []models.UserInfo{
		{UserID: "usr_abc", Username: "admin"},
	}

	mockStore.EXPECT().GetDistinctEventTypes(gomock.Any(), testOrgID).Return(eventTypes, nil)
	mockStore.EXPECT().GetDistinctScopeTypes(gomock.Any(), testOrgID).Return(scopeTypes, nil)
	mockStore.EXPECT().GetDistinctUsers(gomock.Any(), testOrgID).Return(users, nil)

	opts, err := svc.GetFilterOptions(t.Context(), testOrgID)
	require.NoError(t, err)
	assert.Equal(t, eventTypes, opts.EventTypes)
	assert.Equal(t, scopeTypes, opts.ScopeTypes)
	assert.Equal(t, users, opts.Users)
}

func TestService_GetFilterOptions_ErrorPropagation(t *testing.T) {
	t.Run("event types error", func(t *testing.T) {
		svc, mockStore := newTestService(t)
		mockStore.EXPECT().GetDistinctEventTypes(gomock.Any(), testOrgID).Return(nil, errors.New("db error"))
		mockStore.EXPECT().GetDistinctScopeTypes(gomock.Any(), testOrgID).Return(nil, nil)
		mockStore.EXPECT().GetDistinctUsers(gomock.Any(), testOrgID).Return(nil, nil)

		opts, err := svc.GetFilterOptions(t.Context(), testOrgID)
		assert.Nil(t, opts)
		assert.Error(t, err)
	})

	t.Run("scope types error", func(t *testing.T) {
		svc, mockStore := newTestService(t)
		mockStore.EXPECT().GetDistinctEventTypes(gomock.Any(), testOrgID).Return(nil, nil)
		mockStore.EXPECT().GetDistinctScopeTypes(gomock.Any(), testOrgID).Return(nil, errors.New("db error"))
		mockStore.EXPECT().GetDistinctUsers(gomock.Any(), testOrgID).Return(nil, nil)

		opts, err := svc.GetFilterOptions(t.Context(), testOrgID)
		assert.Nil(t, opts)
		assert.Error(t, err)
	})

	t.Run("users error", func(t *testing.T) {
		svc, mockStore := newTestService(t)
		mockStore.EXPECT().GetDistinctEventTypes(gomock.Any(), testOrgID).Return(nil, nil)
		mockStore.EXPECT().GetDistinctScopeTypes(gomock.Any(), testOrgID).Return(nil, nil)
		mockStore.EXPECT().GetDistinctUsers(gomock.Any(), testOrgID).Return(nil, errors.New("db error"))

		opts, err := svc.GetFilterOptions(t.Context(), testOrgID)
		assert.Nil(t, opts)
		assert.Error(t, err)
	})
}
