package auth

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"

	authv1 "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
)

// noopTransactor runs the callback directly without a real DB transaction.
type noopTransactor struct{}

func (noopTransactor) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func (noopTransactor) RunInTxWithResult(ctx context.Context, fn func(ctx context.Context) (any, error)) (any, error) {
	return fn(ctx)
}

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
	for _, user := range m.users {
		if user.ID == userID {
			return user, nil
		}
	}
	return interfaces.User{}, fleeterror.NewNotFoundErrorf("user not found")
}

func (m *mockUserStoreForVerify) GetUserByIDForUpdate(ctx context.Context, userID int64) (interfaces.User, error) {
	return m.GetUserByID(ctx, userID)
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
func (m *mockUserStoreForVerify) CreateAdminUserWithOrganization(ctx context.Context, userID string, username string, passwordHash string, orgName string, orgID string, roleName string, roleDescription string) error {
	return nil
}
func (m *mockUserStoreForVerify) HasUser(ctx context.Context) (bool, error) {
	return false, nil
}
func (m *mockUserStoreForVerify) PasswordUpdatedAt(ctx context.Context, userID int64) (time.Time, error) {
	return time.Time{}, nil
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
			userStore:  &mockUserStoreForVerify{users: map[string]interfaces.User{}},
			transactor: noopTransactor{},
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
		transactor:  noopTransactor{},
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
		transactor:  noopTransactor{},
		activitySvc: activitySvc,
	}

	_, _, err = service.AuthenticateUser(context.Background(), &authv1.AuthenticateRequest{
		Username: "testuser",
		Password: "wrongpassword",
	}, "test-agent", "127.0.0.1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

func TestActivityLogging_LoginFailureConcurrentPasswordRotation(t *testing.T) {
	ctrl := gomock.NewController(t)

	testPassword := "correctpass"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	require.NoError(t, err)

	activitySvc, mockActivityStore := newActivitySvc(ctrl)
	mockUserStore := mocks.NewMockUserStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)

	initialPasswordUpdatedAt := time.Date(2026, 4, 15, 18, 0, 0, 0, time.UTC)
	user := interfaces.User{
		ID:                1,
		UserID:            "ext-user-1",
		Username:          "testuser",
		PasswordHash:      string(hashedPassword),
		PasswordUpdatedAt: initialPasswordUpdatedAt,
	}

	mockUserStore.EXPECT().GetUserByUsername(gomock.Any(), "testuser").Return(user, nil)
	mockUserStore.EXPECT().GetOrganizationsForUser(gomock.Any(), int64(1)).Return([]interfaces.Organization{{ID: 100}}, nil)
	mockTransactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
	mockUserStore.EXPECT().GetUserByIDForUpdate(gomock.Any(), int64(1)).Return(interfaces.User{
		ID:                1,
		UserID:            "ext-user-1",
		Username:          "testuser",
		PasswordHash:      string(hashedPassword),
		PasswordUpdatedAt: initialPasswordUpdatedAt.Add(time.Minute),
	}, nil)
	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			assert.Equal(t, "login_failed", event.Type)
			assert.Equal(t, activitymodels.ResultFailure, event.Result)
			require.NotNil(t, event.ErrorMessage)
			assert.Equal(t, "invalid credentials", *event.ErrorMessage)
			require.NotNil(t, event.UserID)
			assert.Equal(t, "ext-user-1", *event.UserID)
			require.NotNil(t, event.OrganizationID)
			assert.Equal(t, int64(100), *event.OrganizationID)
			return nil
		})

	service := &Service{
		userStore:   mockUserStore,
		transactor:  mockTransactor,
		activitySvc: activitySvc,
	}

	_, _, err = service.AuthenticateUser(context.Background(), &authv1.AuthenticateRequest{
		Username: "testuser",
		Password: testPassword,
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
		transactor:  noopTransactor{},
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

func TestService_UpdatePassword_WrongCurrentPasswordSkipsTransaction(t *testing.T) {
	ctrl := gomock.NewController(t)

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.DefaultCost)
	require.NoError(t, err)

	mockUserStore := mocks.NewMockUserStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)

	mockUserStore.EXPECT().GetUserByID(gomock.Any(), int64(1)).Return(interfaces.User{
		ID:                1,
		PasswordHash:      string(hashedPassword),
		PasswordUpdatedAt: time.Now(),
	}, nil)
	mockUserStore.EXPECT().GetUserByIDForUpdate(gomock.Any(), gomock.Any()).Times(0)
	mockTransactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).Times(0)

	service := &Service{
		userStore:  mockUserStore,
		transactor: mockTransactor,
	}

	_, err = service.UpdatePassword(ctxWithSession("ext-1", "admin", 100), &authv1.UpdatePasswordRequest{
		CurrentPassword: "wrongpass",
		NewPassword:     "newpass123",
	}, "test-agent", "127.0.0.1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid current password")
}

func TestService_UpdatePassword_RejectsConcurrentPasswordRotation(t *testing.T) {
	ctrl := gomock.NewController(t)

	currentPassword := "correctpass"
	hashedCurrentPassword, err := bcrypt.GenerateFromPassword([]byte(currentPassword), bcrypt.DefaultCost)
	require.NoError(t, err)

	mockUserStore := mocks.NewMockUserStore(ctrl)
	mockUserManagementStore := mocks.NewMockUserManagementStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)

	initialPasswordUpdatedAt := time.Date(2026, 4, 15, 18, 0, 0, 0, time.UTC)
	mockUserStore.EXPECT().GetUserByID(gomock.Any(), int64(1)).Return(interfaces.User{
		ID:                1,
		PasswordHash:      string(hashedCurrentPassword),
		PasswordUpdatedAt: initialPasswordUpdatedAt,
	}, nil)
	mockTransactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
	mockUserStore.EXPECT().GetUserByIDForUpdate(gomock.Any(), int64(1)).Return(interfaces.User{
		ID:                1,
		PasswordHash:      string(hashedCurrentPassword),
		PasswordUpdatedAt: initialPasswordUpdatedAt.Add(time.Minute),
	}, nil)
	mockUserManagementStore.EXPECT().
		UpdateUserPasswordAndClearPasswordChangeFlag(gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	service := &Service{
		userStore:           mockUserStore,
		userManagementStore: mockUserManagementStore,
		transactor:          mockTransactor,
	}

	_, err = service.UpdatePassword(ctxWithSession("ext-1", "admin", 100), &authv1.UpdatePasswordRequest{
		CurrentPassword: currentPassword,
		NewPassword:     "newpass123",
	}, "test-agent", "127.0.0.1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid current password")
}

func TestToTimestampProto(t *testing.T) {
	t.Run("returns nil for zero time", func(t *testing.T) {
		result := toTimestampProto(time.Time{})
		assert.Nil(t, result)
	})

	t.Run("returns valid timestamp for non-zero time", func(t *testing.T) {
		now := time.Now()
		result := toTimestampProto(now)
		require.NotNil(t, result)
		assert.Equal(t, now.Unix(), result.Seconds)
	})
}

func TestGetUserAuditInfo_NilTimestampForNeverUpdatedPassword(t *testing.T) {
	ctx := ctxWithSession("ext-123", "admin", 1)

	service := &Service{
		userStore: &mockUserStoreForVerify{
			users: map[string]interfaces.User{},
		},
	}

	resp, err := service.GetUserAuditInfo(ctx)
	require.NoError(t, err)
	require.NotNil(t, resp.Info)
	assert.Nil(t, resp.Info.PasswordUpdatedAt,
		"PasswordUpdatedAt should be nil when password was never updated (DB NULL)")
}

func TestService_VerifySessionCredentials(t *testing.T) {
	testPassword := "testpass123"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	require.NoError(t, err)

	tests := []struct {
		name          string
		username      string
		password      string
		sessionUserID int64
		setupUsers    map[string]interfaces.User
		expectError   bool
		errorContains string
	}{
		{
			name:          "valid credentials matching session user",
			username:      "admin",
			password:      testPassword,
			sessionUserID: 1,
			setupUsers: map[string]interfaces.User{
				"admin": {ID: 1, Username: "admin", PasswordHash: string(hashedPassword)},
			},
			expectError: false,
		},
		{
			name:          "wrong username with correct password",
			username:      "wronguser",
			password:      testPassword,
			sessionUserID: 1,
			setupUsers: map[string]interfaces.User{
				"admin": {ID: 1, Username: "admin", PasswordHash: string(hashedPassword)},
			},
			expectError:   true,
			errorContains: "invalid credentials",
		},
		{
			name:          "correct username with wrong password",
			username:      "admin",
			password:      "wrongpassword",
			sessionUserID: 1,
			setupUsers: map[string]interfaces.User{
				"admin": {ID: 1, Username: "admin", PasswordHash: string(hashedPassword)},
			},
			expectError:   true,
			errorContains: "invalid credentials",
		},
		{
			name:          "another valid user's credentials",
			username:      "bob",
			password:      testPassword,
			sessionUserID: 1,
			setupUsers: map[string]interfaces.User{
				"admin": {ID: 1, Username: "admin", PasswordHash: string(hashedPassword)},
				"bob":   {ID: 2, Username: "bob", PasswordHash: string(hashedPassword)},
			},
			expectError:   true,
			errorContains: "invalid credentials",
		},
		{
			name:          "empty username",
			username:      "",
			password:      testPassword,
			sessionUserID: 1,
			setupUsers:    map[string]interfaces.User{},
			expectError:   true,
			errorContains: "username and password are required",
		},
		{
			name:          "empty password",
			username:      "admin",
			password:      "",
			sessionUserID: 1,
			setupUsers:    map[string]interfaces.User{},
			expectError:   true,
			errorContains: "username and password are required",
		},
		{
			name:          "both empty",
			username:      "",
			password:      "",
			sessionUserID: 1,
			setupUsers:    map[string]interfaces.User{},
			expectError:   true,
			errorContains: "username and password are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &mockUserStoreForVerify{users: tt.setupUsers}
			service := &Service{userStore: mockStore}

			ctx := authn.SetInfo(context.Background(), &session.Info{
				UserID:         tt.sessionUserID,
				OrganizationID: 100,
				ExternalUserID: "ext-1",
				Username:       "admin",
			})

			err := service.VerifySessionCredentials(ctx, tt.username, tt.password)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestService_VerifySessionCredentials_SecurityProperties(t *testing.T) {
	testPassword := "testpass123"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	require.NoError(t, err)

	t.Run("wrong username and wrong password produce identical errors", func(t *testing.T) {
		mockStore := &mockUserStoreForVerify{
			users: map[string]interfaces.User{
				"admin": {ID: 1, Username: "admin", PasswordHash: string(hashedPassword)},
			},
		}
		service := &Service{userStore: mockStore}

		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID:         1,
			OrganizationID: 100,
			ExternalUserID: "ext-1",
			Username:       "admin",
		})

		errWrongUser := service.VerifySessionCredentials(ctx, "wronguser", testPassword)
		errWrongPass := service.VerifySessionCredentials(ctx, "admin", "wrongpassword")

		require.Error(t, errWrongUser)
		require.Error(t, errWrongPass)
		assert.Equal(t, errWrongUser.Error(), errWrongPass.Error(),
			"Error messages should be identical to prevent information leakage")
	})

	t.Run("requires authenticated session", func(t *testing.T) {
		mockStore := &mockUserStoreForVerify{
			users: map[string]interfaces.User{
				"admin": {ID: 1, Username: "admin", PasswordHash: string(hashedPassword)},
			},
		}
		service := &Service{userStore: mockStore}

		err := service.VerifySessionCredentials(context.Background(), "admin", testPassword)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error getting session info")
	})

	t.Run("session user not found in database", func(t *testing.T) {
		mockStore := &mockUserStoreForVerify{users: map[string]interfaces.User{}}
		service := &Service{userStore: mockStore}

		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID:         999,
			OrganizationID: 100,
			ExternalUserID: "ext-999",
			Username:       "ghost",
		})

		err := service.VerifySessionCredentials(ctx, "ghost", testPassword)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error looking up session user")
	})
}

func TestActivityLogging_StepUpAuthFailed(t *testing.T) {
	testPassword := "testpass123"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	require.NoError(t, err)

	t.Run("logs activity on wrong username", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		activitySvc, mockActivityStore := newActivitySvc(ctrl)

		mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
				assert.Equal(t, activitymodels.CategoryAuth, event.Category)
				assert.Equal(t, "step_up_auth_failed", event.Type)
				assert.Equal(t, activitymodels.ResultFailure, event.Result)
				require.NotNil(t, event.ErrorMessage)
				assert.Equal(t, "invalid credentials", *event.ErrorMessage)
				require.NotNil(t, event.Username)
				assert.Equal(t, "admin", *event.Username)
				return nil
			})

		mockStore := &mockUserStoreForVerify{
			users: map[string]interfaces.User{
				"admin": {ID: 1, Username: "admin", PasswordHash: string(hashedPassword)},
			},
		}
		service := &Service{userStore: mockStore, activitySvc: activitySvc}

		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID: 1, OrganizationID: 100,
			ExternalUserID: "ext-1", Username: "admin",
		})

		err := service.VerifySessionCredentials(ctx, "wronguser", testPassword)
		require.Error(t, err)
	})

	t.Run("logs activity on wrong password", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		activitySvc, mockActivityStore := newActivitySvc(ctrl)

		mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
				assert.Equal(t, "step_up_auth_failed", event.Type)
				assert.Equal(t, activitymodels.ResultFailure, event.Result)
				return nil
			})

		mockStore := &mockUserStoreForVerify{
			users: map[string]interfaces.User{
				"admin": {ID: 1, Username: "admin", PasswordHash: string(hashedPassword)},
			},
		}
		service := &Service{userStore: mockStore, activitySvc: activitySvc}

		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID: 1, OrganizationID: 100,
			ExternalUserID: "ext-1", Username: "admin",
		})

		err := service.VerifySessionCredentials(ctx, "admin", "wrongpassword")
		require.Error(t, err)
	})

	t.Run("does not log activity on success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		activitySvc, mockActivityStore := newActivitySvc(ctrl)

		mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

		mockStore := &mockUserStoreForVerify{
			users: map[string]interfaces.User{
				"admin": {ID: 1, Username: "admin", PasswordHash: string(hashedPassword)},
			},
		}
		service := &Service{userStore: mockStore, activitySvc: activitySvc}

		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID: 1, OrganizationID: 100,
			ExternalUserID: "ext-1", Username: "admin",
		})

		err := service.VerifySessionCredentials(ctx, "admin", testPassword)
		require.NoError(t, err)
	})

	t.Run("nil activitySvc does not panic", func(t *testing.T) {
		mockStore := &mockUserStoreForVerify{
			users: map[string]interfaces.User{
				"admin": {ID: 1, Username: "admin", PasswordHash: string(hashedPassword)},
			},
		}
		service := &Service{userStore: mockStore}

		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID: 1, OrganizationID: 100,
			ExternalUserID: "ext-1", Username: "admin",
		})

		assert.NotPanics(t, func() {
			_ = service.VerifySessionCredentials(ctx, "wronguser", testPassword)
		})
	})
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

func TestRequireCallerCanManageTarget(t *testing.T) {
	t.Parallel()

	orgScope := func(perms ...string) authz.Assignment {
		return authz.Assignment{ScopeType: authz.ScopeOrg, Permissions: perms}
	}
	siteScope := func(siteID int64, perms ...string) authz.Assignment {
		sid := siteID
		return authz.Assignment{ScopeType: authz.ScopeSite, SiteID: &sid, Permissions: perms}
	}

	superAdminOrg := []authz.Assignment{
		orgScope("user:read", "user:manage", "role:manage", "miner:reboot", "site:manage", "miner:read", "miner:blink_led"),
	}
	adminOrg := []authz.Assignment{
		orgScope("user:read", "user:manage", "miner:reboot", "site:manage", "miner:read", "miner:blink_led"),
	}
	customOrgWithRoleManage := []authz.Assignment{orgScope("user:read", "role:manage")}
	fieldTechOrg := []authz.Assignment{orgScope("miner:read", "miner:blink_led")}

	// Reviewer-flagged case: ADMIN at org-scope plus a site-scoped
	// custom role granting role:manage *at one site*. The flattened-key
	// approach would let this caller subsume a SUPER_ADMIN target whose
	// role:manage is org-scoped, even though the caller cannot wield
	// role:manage org-wide. Scope-aware comparison must reject.
	adminOrgPlusSiteRoleManage := []authz.Assignment{
		orgScope("user:read", "user:manage", "miner:reboot", "site:manage", "miner:read", "miner:blink_led"),
		siteScope(7, "role:manage"),
	}

	cases := []struct {
		name       string
		caller     []authz.Assignment
		target     []authz.Assignment
		wantDenied bool
	}{
		{"super admin manages super admin (peer via role:manage bypass)", superAdminOrg, superAdminOrg, false},
		{"super admin manages admin", superAdminOrg, adminOrg, false},
		{"super admin manages field tech", superAdminOrg, fieldTechOrg, false},
		{"super admin manages custom-with-role-manage", superAdminOrg, customOrgWithRoleManage, false},
		{"admin manages field tech", adminOrg, fieldTechOrg, false},
		{"admin BLOCKED from peer admin (equality without role:manage)", adminOrg, adminOrg, true},
		{"admin BLOCKED from custom-with-org-role-manage (escalation)", adminOrg, customOrgWithRoleManage, true},
		{"admin cannot manage super admin", adminOrg, superAdminOrg, true},
		{"field tech cannot manage admin", fieldTechOrg, adminOrg, true},
		{"field tech BLOCKED from peer field tech (equality without role:manage)", fieldTechOrg, fieldTechOrg, true},
		{"empty caller cannot manage anyone with perms", nil, fieldTechOrg, true},
		{"anyone manages empty target", adminOrg, nil, false},
		{
			"admin-with-site-scoped-role-manage cannot launder it into org authority over SUPER_ADMIN",
			adminOrgPlusSiteRoleManage, superAdminOrg, true,
		},
		{
			// Caller has org-scope SUPER_ADMIN but narrows to FIELD_TECH at site 7.
			// Target is org-scope ADMIN with no narrowing. Even though the caller's
			// flat org-scope set covers ADMIN's keys, the caller cannot perform
			// ADMIN actions at site 7 (the narrowed FIELD_TECH set excludes them),
			// while the ADMIN target still can. Resetting target's password would
			// hand the caller an account with broader site-7 authority than they
			// themselves possess.
			"caller-side narrowing must block subsumption of an unnarrowed target",
			[]authz.Assignment{
				orgScope("user:read", "user:manage", "role:manage", "miner:reboot", "miner:read", "site:manage"),
				siteScope(7, "miner:read"),
			},
			adminOrg,
			true,
		},
		{
			"site-scoped target requires site-scoped caller authority",
			[]authz.Assignment{orgScope("user:manage")},
			[]authz.Assignment{siteScope(7, "miner:reboot")},
			true,
		},
		{
			"custom role with org-scope role:manage manages peer with same set",
			[]authz.Assignment{orgScope("user:read", "user:manage", "role:manage")},
			[]authz.Assignment{orgScope("user:read", "user:manage", "role:manage")},
			false,
		},
		{
			"admin with operator-added extra perm manages vanilla admin (strict superset)",
			[]authz.Assignment{orgScope("user:read", "user:manage", "miner:reboot", "miner:read", "miner:blink_led", "site:manage", "synthetic:extra")},
			adminOrg,
			false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			caller := authz.NewEffectivePermissions(tc.caller)
			target := authz.NewEffectivePermissions(tc.target)

			err := requireCallerCanManageTarget(caller, target)
			if tc.wantDenied {
				require.Error(t, err)
				var fleetErr fleeterror.FleetError
				require.ErrorAs(t, err, &fleetErr)
				assert.Equal(t, fleeterror.NewForbiddenError("").GRPCCode, fleetErr.GRPCCode,
					"privilege-parity denial should return PermissionDenied")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestResolveCreateUserRole_ValidationBranches covers the pre-LoadEffective
// branches that reject a request before the resolver is touched. The
// "valid role_id assignment lands" and "parity rejection" paths require a
// real PermissionResolver (DB-backed), so they live in the integration
// suite alongside the existing role-management tests.
func TestResolveCreateUserRole_ValidationBranches(t *testing.T) {
	t.Parallel()

	const orgID int64 = 7
	const otherOrgID int64 = 99

	cases := []struct {
		name      string
		roleID    string
		setupMock func(*mocks.MockUserManagementStore)
		wantMsg   string
	}{
		{
			name:      "empty role_id rejected",
			roleID:    "",
			setupMock: func(_ *mocks.MockUserManagementStore) {},
			wantMsg:   "role_id is required",
		},
		{
			name:      "malformed role_id rejected before lookup",
			roleID:    "+1",
			setupMock: func(_ *mocks.MockUserManagementStore) {},
			wantMsg:   "invalid role_id",
		},
		{
			name:   "role not found surfaces as invalid",
			roleID: "42",
			setupMock: func(m *mocks.MockUserManagementStore) {
				m.EXPECT().GetRoleByIDForUpdate(gomock.Any(), int64(42)).Return(interfaces.Role{}, sql.ErrNoRows)
			},
			wantMsg: "invalid role_id",
		},
		{
			name:   "cross-org role rejected",
			roleID: "42",
			setupMock: func(m *mocks.MockUserManagementStore) {
				other := otherOrgID
				m.EXPECT().GetRoleByIDForUpdate(gomock.Any(), int64(42)).Return(interfaces.Role{
					ID:             42,
					Name:           "other-org-admin",
					OrganizationID: &other,
				}, nil)
			},
			wantMsg: "invalid role_id",
		},
		{
			name:   "SUPER_ADMIN built-in rejected",
			roleID: "42",
			setupMock: func(m *mocks.MockUserManagementStore) {
				owner := orgID
				m.EXPECT().GetRoleByIDForUpdate(gomock.Any(), int64(42)).Return(interfaces.Role{
					ID:             42,
					Name:           "Owner",
					OrganizationID: &owner,
					BuiltinKey:     string(authz.BuiltinKeySuperAdmin),
				}, nil)
			},
			wantMsg: "invalid role_id",
		},
		{
			name:   "org-less role rejected",
			roleID: "42",
			setupMock: func(m *mocks.MockUserManagementStore) {
				m.EXPECT().GetRoleByIDForUpdate(gomock.Any(), int64(42)).Return(interfaces.Role{
					ID:   42,
					Name: "global-role",
				}, nil)
			},
			wantMsg: "invalid role_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockStore := mocks.NewMockUserManagementStore(ctrl)
			tc.setupMock(mockStore)

			svc := &Service{userManagementStore: mockStore}
			_, err := svc.resolveCreateUserRole(context.Background(), 1, orgID, tc.roleID)
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, fleeterror.NewInvalidArgumentError("").GRPCCode, fleetErr.GRPCCode)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

// Covers validation branches that fire before the parity check; parity-fail
// and last-SA cases need a DB-backed resolver and live in the integration suite.
func TestUpdateUserRole_ValidationBranches(t *testing.T) {
	t.Parallel()

	const callerExternalID = "caller-ext"
	const callerInternalID int64 = 1
	const orgID int64 = 7
	const otherOrgID int64 = 99
	const targetExternalID = "target-ext"
	const targetInternalID int64 = 42

	target := interfaces.User{ID: targetInternalID, UserID: targetExternalID, Username: "target"}
	owner := orgID

	invalidCode := fleeterror.NewInvalidArgumentError("").GRPCCode

	cases := []struct {
		name    string
		userID  string
		roleID  string
		setup   func(userStore *mocks.MockUserStore, mgmtStore *mocks.MockUserManagementStore)
		wantMsg string
	}{
		{
			name:    "empty user_id rejected",
			userID:  "",
			roleID:  "1",
			setup:   func(_ *mocks.MockUserStore, _ *mocks.MockUserManagementStore) {},
			wantMsg: "user_id is required",
		},
		{
			name:    "empty role_id rejected",
			userID:  targetExternalID,
			roleID:  "",
			setup:   func(_ *mocks.MockUserStore, _ *mocks.MockUserManagementStore) {},
			wantMsg: "role_id is required",
		},
		{
			name:   "unknown target user surfaces as invalid",
			userID: "ghost",
			roleID: "1",
			setup: func(us *mocks.MockUserStore, _ *mocks.MockUserManagementStore) {
				us.EXPECT().GetOrganizationsForUser(gomock.Any(), callerInternalID).
					Return([]interfaces.Organization{{ID: orgID}}, nil)
				us.EXPECT().GetUserByExternalID(gomock.Any(), "ghost").
					Return(interfaces.User{}, sql.ErrNoRows)
			},
			wantMsg: "invalid user_id",
		},
		{
			name:   "malformed role_id rejected inside tx",
			userID: targetExternalID,
			roleID: "+1",
			setup: func(us *mocks.MockUserStore, _ *mocks.MockUserManagementStore) {
				us.EXPECT().GetOrganizationsForUser(gomock.Any(), callerInternalID).
					Return([]interfaces.Organization{{ID: orgID}}, nil)
				us.EXPECT().GetUserByExternalID(gomock.Any(), targetExternalID).Return(target, nil)
			},
			wantMsg: "invalid role_id",
		},
		{
			name:   "role not found surfaces as invalid",
			userID: targetExternalID,
			roleID: "55",
			setup: func(us *mocks.MockUserStore, mgmt *mocks.MockUserManagementStore) {
				us.EXPECT().GetOrganizationsForUser(gomock.Any(), callerInternalID).
					Return([]interfaces.Organization{{ID: orgID}}, nil)
				us.EXPECT().GetUserByExternalID(gomock.Any(), targetExternalID).Return(target, nil)
				mgmt.EXPECT().GetRoleByIDForUpdate(gomock.Any(), int64(55)).
					Return(interfaces.Role{}, sql.ErrNoRows)
			},
			wantMsg: "invalid role_id",
		},
		{
			name:   "cross-org role rejected",
			userID: targetExternalID,
			roleID: "55",
			setup: func(us *mocks.MockUserStore, mgmt *mocks.MockUserManagementStore) {
				us.EXPECT().GetOrganizationsForUser(gomock.Any(), callerInternalID).
					Return([]interfaces.Organization{{ID: orgID}}, nil)
				us.EXPECT().GetUserByExternalID(gomock.Any(), targetExternalID).Return(target, nil)
				other := otherOrgID
				mgmt.EXPECT().GetRoleByIDForUpdate(gomock.Any(), int64(55)).
					Return(interfaces.Role{ID: 55, Name: "other-org-role", OrganizationID: &other}, nil)
			},
			wantMsg: "invalid role_id",
		},
		{
			name:   "SUPER_ADMIN new role rejected",
			userID: targetExternalID,
			roleID: "55",
			setup: func(us *mocks.MockUserStore, mgmt *mocks.MockUserManagementStore) {
				us.EXPECT().GetOrganizationsForUser(gomock.Any(), callerInternalID).
					Return([]interfaces.Organization{{ID: orgID}}, nil)
				us.EXPECT().GetUserByExternalID(gomock.Any(), targetExternalID).Return(target, nil)
				mgmt.EXPECT().GetRoleByIDForUpdate(gomock.Any(), int64(55)).
					Return(interfaces.Role{
						ID:             55,
						Name:           "Owner",
						OrganizationID: &owner,
						BuiltinKey:     string(authz.BuiltinKeySuperAdmin),
					}, nil)
			},
			wantMsg: "invalid role_id",
		},
		{
			// Doubles as cross-org guard: GetOrgScopeAssignmentForUser filters
			// by (user, org), so the same ErrNoRows path covers both cases.
			name:   "target with no live org-scope assignment in caller's org",
			userID: targetExternalID,
			roleID: "55",
			setup: func(us *mocks.MockUserStore, mgmt *mocks.MockUserManagementStore) {
				us.EXPECT().GetOrganizationsForUser(gomock.Any(), callerInternalID).
					Return([]interfaces.Organization{{ID: orgID}}, nil)
				us.EXPECT().GetUserByExternalID(gomock.Any(), targetExternalID).Return(target, nil)
				mgmt.EXPECT().GetRoleByIDForUpdate(gomock.Any(), int64(55)).
					Return(interfaces.Role{ID: 55, Name: "Field Tech", OrganizationID: &owner, BuiltinKey: string(authz.BuiltinKeyFieldTech)}, nil)
				mgmt.EXPECT().GetOrgScopeAssignmentForUser(gomock.Any(), targetInternalID, orgID).
					Return(interfaces.OrgScopeAssignment{}, sql.ErrNoRows)
			},
			wantMsg: "invalid user_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockUserStore := mocks.NewMockUserStore(ctrl)
			mockMgmtStore := mocks.NewMockUserManagementStore(ctrl)
			tc.setup(mockUserStore, mockMgmtStore)

			svc := &Service{
				userStore:           mockUserStore,
				userManagementStore: mockMgmtStore,
				transactor:          noopTransactor{},
			}

			_, err := svc.UpdateUserRole(
				ctxWithSession(callerExternalID, "caller", orgID),
				&authv1.UpdateUserRoleRequest{UserId: tc.userID, RoleId: tc.roleID},
			)
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, invalidCode, fleetErr.GRPCCode)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

func TestUpdateUserRole_NoOpWhenSameRole(t *testing.T) {
	t.Parallel()

	const callerExternalID = "caller-ext"
	const callerInternalID int64 = 1
	const orgID int64 = 7
	const targetExternalID = "target-ext"
	const targetInternalID int64 = 42
	const sameRoleID int64 = 55

	ctrl := gomock.NewController(t)
	target := interfaces.User{ID: targetInternalID, UserID: targetExternalID, Username: "target"}
	owner := orgID

	mockUserStore := mocks.NewMockUserStore(ctrl)
	mockMgmtStore := mocks.NewMockUserManagementStore(ctrl)

	mockUserStore.EXPECT().GetOrganizationsForUser(gomock.Any(), callerInternalID).
		Return([]interfaces.Organization{{ID: orgID}}, nil)
	mockUserStore.EXPECT().GetUserByExternalID(gomock.Any(), targetExternalID).Return(target, nil)
	mockMgmtStore.EXPECT().GetRoleByIDForUpdate(gomock.Any(), sameRoleID).
		Return(interfaces.Role{ID: sameRoleID, Name: "Field Tech", OrganizationID: &owner, BuiltinKey: string(authz.BuiltinKeyFieldTech)}, nil)
	mockMgmtStore.EXPECT().GetOrgScopeAssignmentForUser(gomock.Any(), targetInternalID, orgID).
		Return(interfaces.OrgScopeAssignment{
			AssignmentID: 200,
			RoleID:       sameRoleID,
			BuiltinKey:   string(authz.BuiltinKeyFieldTech),
		}, nil)
	// No UpdateUserOrganizationRole / LoadEffective / Count calls expected — idempotent short-circuit fires first.

	svc := &Service{
		userStore:           mockUserStore,
		userManagementStore: mockMgmtStore,
		transactor:          noopTransactor{},
	}

	resp, err := svc.UpdateUserRole(
		ctxWithSession(callerExternalID, "caller", orgID),
		&authv1.UpdateUserRoleRequest{UserId: targetExternalID, RoleId: fmt.Sprintf("%d", sameRoleID)},
	)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// TestDeactivateUser_LastSuperAdminGuard covers the new in-tx
// last-SUPER_ADMIN guard plus the standard happy / self-deactivate
// branches. Caller is given role:manage so authorizeCallerForUser's
// parity check trivially passes (target is subsumed); the test focus
// is the in-tx guard logic.
func TestDeactivateUser_LastSuperAdminGuard(t *testing.T) {
	t.Parallel()

	const callerExternalID = "caller-ext"
	const callerInternalID int64 = 1
	const orgID int64 = 7
	const targetExternalID = "target-ext"
	const targetInternalID int64 = 42

	target := interfaces.User{ID: targetInternalID, UserID: targetExternalID, Username: "target"}
	caller := interfaces.User{ID: callerInternalID, UserID: callerExternalID, Username: "caller"}
	failedPreCode := fleeterror.NewFailedPreconditionError("").GRPCCode
	invalidArgCode := fleeterror.NewInvalidArgumentError("").GRPCCode
	callerHasRoleManage := orgScopeEff(authz.PermRoleManage, authz.PermUserManage, authz.PermFleetRead)

	cases := []struct {
		name                string
		userID              string
		currentBuiltinKey   string
		saCount             int64
		expectSoftDelete    bool
		expectLockCount     bool
		expectGetAssignment bool
		expectSelfRejectMsg string
		wantErrCode         connect.Code
		wantErrSubstring    string
	}{
		{
			name:                "non-SA target proceeds to SoftDeleteUser",
			userID:              targetExternalID,
			currentBuiltinKey:   string(authz.BuiltinKeyFieldTech),
			expectGetAssignment: true,
			expectSoftDelete:    true,
		},
		{
			name:                "SA target refused when count would drop to zero",
			userID:              targetExternalID,
			currentBuiltinKey:   string(authz.BuiltinKeySuperAdmin),
			saCount:             1,
			expectGetAssignment: true,
			expectLockCount:     true,
			wantErrCode:         failedPreCode,
			wantErrSubstring:    "cannot deactivate the last SUPER_ADMIN",
		},
		{
			name:                "SA target proceeds when another SA remains",
			userID:              targetExternalID,
			currentBuiltinKey:   string(authz.BuiltinKeySuperAdmin),
			saCount:             2,
			expectGetAssignment: true,
			expectLockCount:     true,
			expectSoftDelete:    true,
		},
		{
			// Self-deactivation: rejected before any store calls beyond the
			// initial caller lookup. The handler-layer test exercises the
			// permission gate; this asserts the domain-layer CANNOT_DEACTIVATE_SELF.
			name:             "self-deactivate rejected before tx",
			userID:           callerExternalID,
			wantErrCode:      invalidArgCode,
			wantErrSubstring: "cannot deactivate your own account",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockUserStore := mocks.NewMockUserStore(ctrl)
			mockMgmtStore := mocks.NewMockUserManagementStore(ctrl)

			mockUserStore.EXPECT().GetOrganizationsForUser(gomock.Any(), callerInternalID).
				Return([]interfaces.Organization{{ID: orgID}}, nil)
			mockUserStore.EXPECT().GetUserByID(gomock.Any(), callerInternalID).Return(caller, nil)

			if tc.expectGetAssignment {
				// authorizeCallerForUser → target lookup + parity check
				mockUserStore.EXPECT().GetUserByExternalID(gomock.Any(), targetExternalID).Return(target, nil)
				mockMgmtStore.EXPECT().GetUserRoleName(gomock.Any(), targetInternalID, orgID).Return("FIELD_TECH", nil)
				// In-tx assignment fetch
				mockMgmtStore.EXPECT().GetOrgScopeAssignmentForUser(gomock.Any(), targetInternalID, orgID).
					Return(interfaces.OrgScopeAssignment{
						AssignmentID: 200,
						RoleID:       999,
						BuiltinKey:   tc.currentBuiltinKey,
					}, nil)
			}
			if tc.expectLockCount {
				mockMgmtStore.EXPECT().LockAndCountOrgScopeSuperAdmins(gomock.Any(), orgID).
					Return(tc.saCount, nil)
			}
			if tc.expectSoftDelete {
				mockMgmtStore.EXPECT().SoftDeleteUser(gomock.Any(), targetInternalID).Return(nil)
			}

			svc := &Service{
				userStore:           mockUserStore,
				userManagementStore: mockMgmtStore,
				transactor:          noopTransactor{},
				permResolver: &fakeResolver{effective: map[int64]*authz.EffectivePermissions{
					callerInternalID: callerHasRoleManage,
					targetInternalID: orgScopeEff(authz.PermFleetRead), // trivially subsumed
				}},
			}

			_, err := svc.DeactivateUser(
				ctxWithSession(callerExternalID, "caller", orgID),
				&authv1.DeactivateUserRequest{UserId: tc.userID},
			)

			if tc.wantErrCode == 0 {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, tc.wantErrCode, fleetErr.GRPCCode)
			assert.Contains(t, err.Error(), tc.wantErrSubstring)
		})
	}
}

// fakeResolver stubs the parity-check dependency so unit tests can drive
// the parity / last-SUPER_ADMIN branches without a DB-backed resolver.
// effective keyed by userID; absent users return an empty (deny-all) set.
type fakeResolver struct {
	effective map[int64]*authz.EffectivePermissions
}

func (f *fakeResolver) LoadEffective(_ context.Context, userID, _ int64) (*authz.EffectivePermissions, error) {
	if e, ok := f.effective[userID]; ok {
		return e, nil
	}
	return authz.NewEffectivePermissions(nil), nil
}

func (f *fakeResolver) LoadEffectiveForUpdateInTx(ctx context.Context, userID, organizationID int64) (*authz.EffectivePermissions, error) {
	return f.LoadEffective(ctx, userID, organizationID)
}

func orgScopeEff(perms ...string) *authz.EffectivePermissions {
	return authz.NewEffectivePermissions([]authz.Assignment{{
		ScopeType:   authz.ScopeOrg,
		Permissions: perms,
	}})
}

// TestUpdateUserRole_ParityAndLastSuperAdmin covers the branches that fire
// after GetOrgScopeAssignmentForUser: parity vs target's current perms,
// parity vs the new role's perms, and the last-SUPER_ADMIN guard. The
// resolver is stubbed via fakeResolver so each case can hand-craft the
// caller/target effective sets.
func TestUpdateUserRole_ParityAndLastSuperAdmin(t *testing.T) {
	t.Parallel()

	const callerExternalID = "caller-ext"
	const callerInternalID int64 = 1
	const orgID int64 = 7
	const targetExternalID = "target-ext"
	const targetInternalID int64 = 42
	const newRoleID int64 = 55

	target := interfaces.User{ID: targetInternalID, UserID: targetExternalID, Username: "target"}
	owner := orgID

	invalidArgCode := fleeterror.NewInvalidArgumentError("").GRPCCode
	_ = invalidArgCode
	forbiddenCode := fleeterror.NewForbiddenError("").GRPCCode
	failedPreCode := fleeterror.NewFailedPreconditionError("").GRPCCode

	// Builds a Service with mocks wired up for the standard "lookup +
	// in-tx role resolve" path. Tests override callerEff/targetEff/newRoleKeys
	// and the last-SA count to exercise specific branches.
	type setup struct {
		callerEff   *authz.EffectivePermissions
		targetEff   *authz.EffectivePermissions
		newRoleKeys []string
		// builtinKey of the target's *current* assignment row (drives whether
		// the last-SA guard runs).
		currentBuiltinKey string
		// remaining live SUPER_ADMIN count returned by LockAndCount when the
		// guard runs. Set to 0 when the current assignment isn't SA — the
		// guard won't be called.
		saCount int64
		// expectSwap when true, the test expects the swap write to fire and
		// returns nil from it; when false, the swap must NOT be called.
		expectSwap bool
	}

	cases := []struct {
		name     string
		s        setup
		wantCode connect.Code
		wantMsg  string
	}{
		{
			name: "parity-fail on current role: peer caller cannot manage target",
			s: setup{
				// Caller and target both hold the same org-scope perms, so caller
				// doesn't strictly dominate (no role:manage shortcut, equal perms).
				callerEff:         orgScopeEff(authz.PermUserManage, authz.PermFleetRead),
				targetEff:         orgScopeEff(authz.PermUserManage, authz.PermFleetRead),
				newRoleKeys:       []string{authz.PermFleetRead},
				currentBuiltinKey: string(authz.BuiltinKeyAdmin),
			},
			wantCode: forbiddenCode,
			wantMsg:  "insufficient permissions to manage this user",
		},
		{
			name: "parity-fail on new role: caller dominates current but not new",
			s: setup{
				// Caller dominates target's current set (target has only fleet:read).
				callerEff:   orgScopeEff(authz.PermUserManage, authz.PermFleetRead),
				targetEff:   orgScopeEff(authz.PermFleetRead),
				newRoleKeys: []string{authz.PermUserManage, authz.PermRoleManage, authz.PermFleetRead},
				// New role grants role:manage which caller lacks — parity-on-new fails.
				currentBuiltinKey: string(authz.BuiltinKeyFieldTech),
			},
			wantCode: forbiddenCode,
			wantMsg:  "insufficient permissions to manage this user",
		},
		{
			name: "last-SUPER_ADMIN refusal: target is SA and count would drop to zero",
			s: setup{
				// Caller has role:manage so it can manage the SA target (parity passes).
				callerEff:         orgScopeEff(authz.PermRoleManage, authz.PermUserManage, authz.PermFleetRead),
				targetEff:         orgScopeEff(authz.PermRoleManage, authz.PermUserManage, authz.PermFleetRead),
				newRoleKeys:       []string{authz.PermFleetRead},
				currentBuiltinKey: string(authz.BuiltinKeySuperAdmin),
				saCount:           1,
			},
			wantCode: failedPreCode,
			wantMsg:  "cannot demote the last SUPER_ADMIN",
		},
		{
			name: "last-SUPER_ADMIN passes when another SA remains: swap proceeds",
			s: setup{
				callerEff:         orgScopeEff(authz.PermRoleManage, authz.PermUserManage, authz.PermFleetRead),
				targetEff:         orgScopeEff(authz.PermRoleManage, authz.PermUserManage, authz.PermFleetRead),
				newRoleKeys:       []string{authz.PermFleetRead},
				currentBuiltinKey: string(authz.BuiltinKeySuperAdmin),
				saCount:           2,
				expectSwap:        true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockUserStore := mocks.NewMockUserStore(ctrl)
			mockMgmtStore := mocks.NewMockUserManagementStore(ctrl)

			mockUserStore.EXPECT().GetOrganizationsForUser(gomock.Any(), callerInternalID).
				Return([]interfaces.Organization{{ID: orgID}}, nil)
			mockUserStore.EXPECT().GetUserByExternalID(gomock.Any(), targetExternalID).Return(target, nil)
			mockMgmtStore.EXPECT().GetRoleByIDForUpdate(gomock.Any(), newRoleID).
				Return(interfaces.Role{ID: newRoleID, Name: "FIELD_TECH", OrganizationID: &owner, BuiltinKey: string(authz.BuiltinKeyFieldTech)}, nil)
			mockMgmtStore.EXPECT().GetOrgScopeAssignmentForUser(gomock.Any(), targetInternalID, orgID).
				Return(interfaces.OrgScopeAssignment{
					AssignmentID: 200,
					RoleID:       999, // != newRoleID so the no-op short-circuit doesn't fire
					BuiltinKey:   tc.s.currentBuiltinKey,
				}, nil)
			mockMgmtStore.EXPECT().ListPermissionKeysByRoleID(gomock.Any(), newRoleID).
				Return(tc.s.newRoleKeys, nil)

			if tc.s.currentBuiltinKey == string(authz.BuiltinKeySuperAdmin) {
				// Parity check passes when caller has role:manage and target's
				// perms are subsumed — both SA scenarios are configured to pass.
				mockMgmtStore.EXPECT().LockAndCountOrgScopeSuperAdmins(gomock.Any(), orgID).
					Return(tc.s.saCount, nil)
			}
			if tc.s.expectSwap {
				mockMgmtStore.EXPECT().UpdateUserOrganizationRole(gomock.Any(), targetInternalID, orgID, int64(200), newRoleID).
					Return(nil)
			}

			svc := &Service{
				userStore:           mockUserStore,
				userManagementStore: mockMgmtStore,
				transactor:          noopTransactor{},
				permResolver: &fakeResolver{effective: map[int64]*authz.EffectivePermissions{
					callerInternalID: tc.s.callerEff,
					targetInternalID: tc.s.targetEff,
				}},
			}

			_, err := svc.UpdateUserRole(
				ctxWithSession(callerExternalID, "caller", orgID),
				&authv1.UpdateUserRoleRequest{UserId: targetExternalID, RoleId: fmt.Sprintf("%d", newRoleID)},
			)

			if tc.wantCode == 0 {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)
			if tc.wantMsg != "" {
				assert.Contains(t, err.Error(), tc.wantMsg)
			}
		})
	}
}
