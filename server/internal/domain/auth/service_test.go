package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/proto-at-block/proto-fleet/server/internal/domain/activity/models"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/session"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"

	authv1 "github.com/proto-at-block/proto-fleet/server/generated/grpc/auth/v1"
)

type mockUserStoreForVerify struct {
	users         map[string]interfaces.User
	orgs          []interfaces.Organization
	lookupErr     error
	updateUserErr error
}

func (m *mockUserStoreForVerify) GetUserByUsername(ctx context.Context, username string) (interfaces.User, error) {
	if m.lookupErr != nil {
		return interfaces.User{}, m.lookupErr
	}
	user, exists := m.users[username]
	if !exists {
		return interfaces.User{}, fleeterror.NewNotFoundErrorf("user not found")
	}
	return user, nil
}

func (m *mockUserStoreForVerify) GetUserByID(ctx context.Context, userID int64) (interfaces.User, error) {
	return interfaces.User{}, nil
}
func (m *mockUserStoreForVerify) GetUserByExternalID(ctx context.Context, userID string) (interfaces.User, error) {
	return interfaces.User{}, nil
}
func (m *mockUserStoreForVerify) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	return nil
}
func (m *mockUserStoreForVerify) UpdateUserUsername(ctx context.Context, userID int64, username string) error {
	return m.updateUserErr
}
func (m *mockUserStoreForVerify) GetOrganizationsForUser(ctx context.Context, userID int64) ([]interfaces.Organization, error) {
	return m.orgs, nil
}
func (m *mockUserStoreForVerify) CreateAdminUserWithOrganization(ctx context.Context, userID string, username string, passwordHash string, orgName string, orgID string, minerAuthPrivateKey string, roleName string, roleDescription string) error {
	return nil
}
func (m *mockUserStoreForVerify) HasUser(ctx context.Context) (bool, error) {
	return false, nil
}
func (m *mockUserStoreForVerify) PasswordUpdatedAt(ctx context.Context, userID int64) (time.Time, error) {
	return time.Time{}, nil
}
func (m *mockUserStoreForVerify) GetOrganizationPrivateKey(ctx context.Context, orgID int64) (string, error) {
	return "", nil
}

func newActivitySvc(ctrl *gomock.Controller) (*activity.Service, *mocks.MockActivityStore) {
	mockStore := mocks.NewMockActivityStore(ctrl)
	return activity.NewService(mockStore), mockStore
}

func ctxWithSession(externalUserID, username string, orgID int64) context.Context {
	return authn.SetInfo(context.Background(), &session.Info{
		SessionID:      "test-session",
		UserID:         1,
		OrganizationID: orgID,
		ExternalUserID: externalUserID,
		Username:       username,
	})
}

func TestService_VerifyCredentials(t *testing.T) {
	// Create test password hash
	testPassword := "testpass123"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	require.NoError(t, err)

	tests := []struct {
		name          string
		username      string
		password      string
		setupUsers    map[string]interfaces.User
		expectError   bool
		errorContains string
	}{
		{
			name:     "valid credentials",
			username: "testuser",
			password: testPassword,
			setupUsers: map[string]interfaces.User{
				"testuser": {
					ID:           1,
					Username:     "testuser",
					PasswordHash: string(hashedPassword),
				},
			},
			expectError: false,
		},
		{
			name:     "invalid password",
			username: "testuser",
			password: "wrongpassword",
			setupUsers: map[string]interfaces.User{
				"testuser": {
					ID:           1,
					Username:     "testuser",
					PasswordHash: string(hashedPassword),
				},
			},
			expectError:   true,
			errorContains: "invalid credentials",
		},
		{
			name:          "user not found",
			username:      "nonexistent",
			password:      testPassword,
			setupUsers:    map[string]interfaces.User{},
			expectError:   true,
			errorContains: "invalid credentials",
		},
		{
			name:          "empty username",
			username:      "",
			password:      testPassword,
			setupUsers:    map[string]interfaces.User{},
			expectError:   true,
			errorContains: "username and password are required",
		},
		{
			name:     "empty password",
			username: "testuser",
			password: "",
			setupUsers: map[string]interfaces.User{
				"testuser": {
					ID:           1,
					Username:     "testuser",
					PasswordHash: string(hashedPassword),
				},
			},
			expectError:   true,
			errorContains: "username and password are required",
		},
		{
			name:          "both empty",
			username:      "",
			password:      "",
			setupUsers:    map[string]interfaces.User{},
			expectError:   true,
			errorContains: "username and password are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock user store
			mockStore := &mockUserStoreForVerify{
				users: tt.setupUsers,
			}

			// Create auth service with mock store
			service := &Service{
				userStore: mockStore,
			}

			// Call VerifyCredentials
			err := service.VerifyCredentials(context.Background(), tt.username, tt.password)

			// Assert results
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestService_VerifyCredentials_SecurityProperties(t *testing.T) {
	t.Run("does not leak user existence through timing or error messages", func(t *testing.T) {
		// Create test password hash
		testPassword := "testpass123"
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
		require.NoError(t, err)

		mockStore := &mockUserStoreForVerify{
			users: map[string]interfaces.User{
				"existinguser": {
					ID:           1,
					Username:     "existinguser",
					PasswordHash: string(hashedPassword),
				},
			},
		}

		service := &Service{
			userStore: mockStore,
		}

		// Try with non-existent user
		err1 := service.VerifyCredentials(context.Background(), "nonexistent", testPassword)
		require.Error(t, err1)

		// Try with wrong password for existing user
		err2 := service.VerifyCredentials(context.Background(), "existinguser", "wrongpass")
		require.Error(t, err2)

		// Both should return same generic error message
		assert.Equal(t, err1.Error(), err2.Error(), "Error messages should not leak user existence")
		assert.Contains(t, err1.Error(), "invalid credentials")
	})

	t.Run("prevents empty credential bypass", func(t *testing.T) {
		service := &Service{
			userStore: &mockUserStoreForVerify{
				users: map[string]interfaces.User{},
			},
		}

		// All empty credential combinations should fail
		testCases := []struct {
			username string
			password string
		}{
			{"", ""},
			{"", "password"},
			{"username", ""},
		}

		for _, tc := range testCases {
			err := service.VerifyCredentials(context.Background(), tc.username, tc.password)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "username and password are required")
		}
	})
}

func TestActivityLogging_NilActivitySvc(t *testing.T) {
	t.Run("login failure with nil activitySvc does not panic", func(t *testing.T) {
		service := &Service{
			userStore: &mockUserStoreForVerify{users: map[string]interfaces.User{}},
		}

		assert.NotPanics(t, func() {
			_, _, err := service.AuthenticateUser(context.Background(), &authv1.AuthenticateRequest{
				Username: "nonexistent",
				Password: "password",
			}, "test-agent", "127.0.0.1")
			require.Error(t, err)
		})
	})

	t.Run("UpdateUsername with nil activitySvc does not panic", func(t *testing.T) {
		ctx := ctxWithSession("ext-123", "admin", 1)
		service := &Service{
			userStore: &mockUserStoreForVerify{users: map[string]interfaces.User{}},
		}

		assert.NotPanics(t, func() {
			_ = service.UpdateUsername(ctx, "newname")
		})
	})
}

func TestActivityLogging_LoginFailureUserNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	activitySvc, mockActivityStore := newActivitySvc(ctrl)

	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			assert.Equal(t, activitymodels.CategoryAuth, event.Category)
			assert.Equal(t, "login_failed", event.Type)
			assert.Equal(t, activitymodels.ResultFailure, event.Result)
			assert.Nil(t, event.UserID, "UserID should be nil for unknown user")
			assert.Nil(t, event.OrganizationID, "OrganizationID should be nil for unknown user")
			require.NotNil(t, event.Username)
			assert.Equal(t, "nonexistent", *event.Username)
			return nil
		})

	service := &Service{
		userStore:   &mockUserStoreForVerify{users: map[string]interfaces.User{}},
		activitySvc: activitySvc,
	}

	_, _, err := service.AuthenticateUser(context.Background(), &authv1.AuthenticateRequest{
		Username: "nonexistent",
		Password: "password",
	}, "test-agent", "127.0.0.1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

func TestActivityLogging_LoginFailureWrongPassword(t *testing.T) {
	ctrl := gomock.NewController(t)

	testPassword := "correctpass"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	require.NoError(t, err)

	activitySvc, mockActivityStore := newActivitySvc(ctrl)

	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			assert.Equal(t, "login_failed", event.Type)
			require.NotNil(t, event.UserID)
			assert.Equal(t, "ext-user-1", *event.UserID)
			require.NotNil(t, event.OrganizationID)
			assert.Equal(t, int64(100), *event.OrganizationID)
			return nil
		})

	service := &Service{
		userStore: &mockUserStoreForVerify{
			users: map[string]interfaces.User{
				"testuser": {
					ID:           1,
					UserID:       "ext-user-1",
					Username:     "testuser",
					PasswordHash: string(hashedPassword),
				},
			},
			orgs: []interfaces.Organization{{ID: 100}},
		},
		activitySvc: activitySvc,
	}

	_, _, err = service.AuthenticateUser(context.Background(), &authv1.AuthenticateRequest{
		Username: "testuser",
		Password: "wrongpassword",
	}, "test-agent", "127.0.0.1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

func TestActivityLogging_DBErrorReturnsInternalNotLoginFailed(t *testing.T) {
	ctrl := gomock.NewController(t)

	activitySvc, mockActivityStore := newActivitySvc(ctrl)
	// Insert should NOT be called for DB errors
	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

	service := &Service{
		userStore: &mockUserStoreForVerify{
			users:     map[string]interfaces.User{},
			lookupErr: fmt.Errorf("connection refused"),
		},
		activitySvc: activitySvc,
	}

	_, _, err := service.AuthenticateUser(context.Background(), &authv1.AuthenticateRequest{
		Username: "anyuser",
		Password: "password",
	}, "test-agent", "127.0.0.1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication service unavailable")
	assert.NotContains(t, err.Error(), "connection refused")
}

func TestActivityLogging_UpdateUsernameLogsOldAndNew(t *testing.T) {
	ctrl := gomock.NewController(t)

	activitySvc, mockActivityStore := newActivitySvc(ctrl)

	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			assert.Equal(t, "update_username", event.Type)
			require.NotNil(t, event.Username)
			assert.Equal(t, "oldname", *event.Username)
			require.NotNil(t, event.Metadata)
			assert.Equal(t, "oldname", event.Metadata["old_username"])
			assert.Equal(t, "newname", event.Metadata["new_username"])
			return nil
		})

	ctx := ctxWithSession("ext-123", "oldname", 1)
	service := &Service{
		userStore:   &mockUserStoreForVerify{users: map[string]interfaces.User{}},
		activitySvc: activitySvc,
	}

	err := service.UpdateUsername(ctx, "newname")
	require.NoError(t, err)
}
