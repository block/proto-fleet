package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
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
