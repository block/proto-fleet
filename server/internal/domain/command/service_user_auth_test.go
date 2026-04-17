package command

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCredentialsVerifier for testing
type mockCredentialsVerifier struct {
	shouldFail bool
	failError  error
}

func (m *mockCredentialsVerifier) VerifyCredentials(ctx context.Context, username, password string) error {
	if m.shouldFail {
		return m.failError
	}
	return nil
}

// mockUserStoreForAuth for testing
type mockUserStoreForAuth struct {
	users map[string]interfaces.User
}

func (m *mockUserStoreForAuth) GetUserByUsername(ctx context.Context, username string) (interfaces.User, error) {
	user, exists := m.users[username]
	if !exists {
		return interfaces.User{}, fleeterror.NewNotFoundErrorf("user not found")
	}
	return user, nil
}

// Implement other UserStore methods (not used in these tests)
func (m *mockUserStoreForAuth) GetUserByID(ctx context.Context, userID int64) (interfaces.User, error) {
	return interfaces.User{}, nil
}

func (m *mockUserStoreForAuth) GetUserByIDForUpdate(ctx context.Context, userID int64) (interfaces.User, error) {
	return m.GetUserByID(ctx, userID)
}
func (m *mockUserStoreForAuth) GetUserByExternalID(ctx context.Context, userID string) (interfaces.User, error) {
	return interfaces.User{}, nil
}
func (m *mockUserStoreForAuth) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	return nil
}
func (m *mockUserStoreForAuth) UpdateUserUsername(ctx context.Context, userID int64, username string) error {
	return nil
}
func (m *mockUserStoreForAuth) GetOrganizationsForUser(ctx context.Context, userID int64) ([]interfaces.Organization, error) {
	return nil, nil
}
func (m *mockUserStoreForAuth) CreateAdminUserWithOrganization(ctx context.Context, userID string, username string, passwordHash string, orgName string, orgID string, minerAuthPrivateKey string, roleName string, roleDescription string) error {
	return nil
}
func (m *mockUserStoreForAuth) HasUser(ctx context.Context) (bool, error) {
	return false, nil
}
func (m *mockUserStoreForAuth) PasswordUpdatedAt(ctx context.Context, userID int64) (time.Time, error) {
	return time.Time{}, nil
}
func (m *mockUserStoreForAuth) GetOrganizationPrivateKey(ctx context.Context, orgID int64) (string, error) {
	return "", nil
}

func TestService_verifyUserCredentials(t *testing.T) {
	tests := []struct {
		name              string
		username          string
		password          string
		sessionUserID     int64
		sessionOrgID      int64
		setupUsers        map[string]interfaces.User
		credVerifierFails bool
		credVerifierError error
		expectError       bool
		errorContains     string
		errorType         string // "forbidden", "internal", etc.
	}{
		{
			name:          "valid credentials matching session user",
			username:      "testuser",
			password:      "password123",
			sessionUserID: 1,
			sessionOrgID:  100,
			setupUsers: map[string]interfaces.User{
				"testuser": {
					ID:       1,
					Username: "testuser",
				},
			},
			credVerifierFails: false,
			expectError:       false,
		},
		{
			name:              "invalid credentials",
			username:          "testuser",
			password:          "wrongpassword",
			sessionUserID:     1,
			sessionOrgID:      100,
			setupUsers:        map[string]interfaces.User{},
			credVerifierFails: true,
			credVerifierError: fleeterror.NewForbiddenErrorf("invalid credentials"),
			expectError:       true,
			errorContains:     "invalid credentials",
			errorType:         "forbidden",
		},
		{
			name:          "valid credentials but username mismatch",
			username:      "attacker",
			password:      "attackerpass",
			sessionUserID: 1, // Logged in as user ID 1
			sessionOrgID:  100,
			setupUsers: map[string]interfaces.User{
				"attacker": {
					ID:       2, // But providing credentials for user ID 2
					Username: "attacker",
				},
			},
			credVerifierFails: false, // Credentials are valid
			expectError:       true,
			errorContains:     "username does not match authenticated user",
			errorType:         "forbidden",
		},
		{
			name:              "user not found in database",
			username:          "nonexistent",
			password:          "password",
			sessionUserID:     1,
			sessionOrgID:      100,
			setupUsers:        map[string]interfaces.User{}, // No users
			credVerifierFails: false,
			expectError:       true,
			errorContains:     "error getting user",
			errorType:         "internal",
		},
		{
			name:              "empty username",
			username:          "",
			password:          "password123",
			sessionUserID:     1,
			sessionOrgID:      100,
			setupUsers:        map[string]interfaces.User{},
			credVerifierFails: false, // Not reached - validation fails first
			expectError:       true,
			errorContains:     "user_username is required",
			errorType:         "invalid_argument",
		},
		{
			name:              "empty password",
			username:          "testuser",
			password:          "",
			sessionUserID:     1,
			sessionOrgID:      100,
			setupUsers:        map[string]interfaces.User{},
			credVerifierFails: false, // Not reached - validation fails first
			expectError:       true,
			errorContains:     "user_password is required",
			errorType:         "invalid_argument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock credentials verifier
			mockVerifier := &mockCredentialsVerifier{
				shouldFail: tt.credVerifierFails,
				failError:  tt.credVerifierError,
			}

			// Create mock user store
			mockUserStore := &mockUserStoreForAuth{
				users: tt.setupUsers,
			}

			// Create service with mocks
			service := &Service{
				credentialsVerifier: mockVerifier,
				userStore:           mockUserStore,
			}

			// Create context with session info
			ctx := context.Background()
			if tt.sessionUserID > 0 {
				ctx = authn.SetInfo(ctx, &session.Info{
					UserID:         tt.sessionUserID,
					OrganizationID: tt.sessionOrgID,
				})
			}

			// Call verifyUserCredentials
			err := service.verifyUserCredentials(ctx, tt.username, tt.password)

			// Assert results
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)

				// Check error type
				if tt.errorType == "forbidden" {
					var fleetErr fleeterror.FleetError
					if assert.ErrorAs(t, err, &fleetErr, "Should be a FleetError") {
						assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode, "Should be a PermissionDenied error")
					}
				} else if tt.errorType == "internal" {
					var fleetErr fleeterror.FleetError
					if assert.ErrorAs(t, err, &fleetErr, "Should be a FleetError") {
						assert.Equal(t, connect.CodeInternal, fleetErr.GRPCCode, "Should be an Internal error")
					}
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestService_verifyUserCredentials_SecurityScenarios(t *testing.T) {
	t.Run("prevents cross-user credential usage", func(t *testing.T) {
		// Scenario: Alice is logged in (user ID 1) but provides Bob's credentials (user ID 2)
		// This should be rejected even though Bob's credentials are valid

		mockVerifier := &mockCredentialsVerifier{
			shouldFail: false, // Bob's credentials are valid
		}

		mockUserStore := &mockUserStoreForAuth{
			users: map[string]interfaces.User{
				"alice": {ID: 1, Username: "alice"},
				"bob":   {ID: 2, Username: "bob"},
			},
		}

		service := &Service{
			credentialsVerifier: mockVerifier,
			userStore:           mockUserStore,
		}

		// Alice is logged in (session user ID = 1)
		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID:         1,
			OrganizationID: 100,
		})

		// But she provides Bob's username and password
		err := service.verifyUserCredentials(ctx, "bob", "bobspassword")

		// Should fail with forbidden error
		require.Error(t, err)
		assert.Contains(t, err.Error(), "username does not match authenticated user")

		var fleetErr fleeterror.FleetError
		if assert.ErrorAs(t, err, &fleetErr, "Should be a FleetError") {
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode, "Should be a PermissionDenied error")
		}
	})

	t.Run("requires authenticated session", func(t *testing.T) {
		service := &Service{
			credentialsVerifier: &mockCredentialsVerifier{shouldFail: false},
			userStore: &mockUserStoreForAuth{
				users: map[string]interfaces.User{
					"testuser": {ID: 1, Username: "testuser"},
				},
			},
		}

		// Context without session info
		ctx := context.Background()

		err := service.verifyUserCredentials(ctx, "testuser", "password")

		// Should fail when trying to get session info
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error getting session info")
	})

	t.Run("validates credentials before checking session match", func(t *testing.T) {
		// This ensures we don't leak information about whether the username
		// matches the session before validating the credentials

		mockVerifier := &mockCredentialsVerifier{
			shouldFail: true,
			failError:  fleeterror.NewForbiddenErrorf("invalid credentials"),
		}

		service := &Service{
			credentialsVerifier: mockVerifier,
			userStore:           &mockUserStoreForAuth{users: map[string]interfaces.User{}},
		}

		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID:         1,
			OrganizationID: 100,
		})

		// Even if username would mismatch, we should see credential error first
		err := service.verifyUserCredentials(ctx, "wronguser", "wrongpass")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid credentials")
	})
}

func TestService_UpdateMinerPassword_WithUserAuthentication(t *testing.T) {
	t.Run("rejects update with invalid user credentials", func(t *testing.T) {
		mockVerifier := &mockCredentialsVerifier{
			shouldFail: true,
			failError:  fleeterror.NewForbiddenErrorf("invalid credentials"),
		}

		service := &Service{
			credentialsVerifier: mockVerifier,
			userStore:           &mockUserStoreForAuth{users: map[string]interfaces.User{}},
		}

		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID:         1,
			OrganizationID: 100,
		})

		_, err := service.UpdateMinerPassword(
			ctx,
			nil, // device selector
			"newpass",
			"currentpass",
			"testuser",
			"wrongpass",
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid credentials")
	})

	t.Run("rejects update when username does not match session", func(t *testing.T) {
		mockVerifier := &mockCredentialsVerifier{
			shouldFail: false, // Credentials are valid
		}

		mockUserStore := &mockUserStoreForAuth{
			users: map[string]interfaces.User{
				"attacker": {ID: 2, Username: "attacker"},
			},
		}

		service := &Service{
			credentialsVerifier: mockVerifier,
			userStore:           mockUserStore,
		}

		// User 1 is logged in
		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID:         1,
			OrganizationID: 100,
		})

		// But provides credentials for user 2
		_, err := service.UpdateMinerPassword(
			ctx,
			nil,
			"newpass",
			"currentpass",
			"attacker", // User ID 2
			"attackerpass",
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "username does not match authenticated user")

		var fleetErr fleeterror.FleetError
		if assert.ErrorAs(t, err, &fleetErr, "Should be a FleetError") {
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode, "Should be a PermissionDenied error")
		}
	})
}

func TestService_UpdateMiningPools_WithUserAuthentication(t *testing.T) {
	t.Run("rejects update with invalid user credentials", func(t *testing.T) {
		mockVerifier := &mockCredentialsVerifier{
			shouldFail: true,
			failError:  fleeterror.NewForbiddenErrorf("invalid credentials"),
		}

		service := &Service{
			credentialsVerifier: mockVerifier,
			userStore:           &mockUserStoreForAuth{users: map[string]interfaces.User{}},
		}

		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID:         1,
			OrganizationID: 100,
		})

		_, err := service.UpdateMiningPools(
			ctx,
			nil, // device selector
			nil, // default pool
			nil, // backup 1 pool
			nil, // backup 2 pool
			"testuser",
			"wrongpass",
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid credentials")
	})

	t.Run("rejects update when username does not match session", func(t *testing.T) {
		mockVerifier := &mockCredentialsVerifier{
			shouldFail: false, // Credentials are valid
		}

		mockUserStore := &mockUserStoreForAuth{
			users: map[string]interfaces.User{
				"attacker": {ID: 2, Username: "attacker"},
			},
		}

		service := &Service{
			credentialsVerifier: mockVerifier,
			userStore:           mockUserStore,
		}

		// User 1 is logged in
		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID:         1,
			OrganizationID: 100,
		})

		// But provides credentials for user 2
		_, err := service.UpdateMiningPools(
			ctx,
			nil,
			nil,
			nil,
			nil,
			"attacker", // User ID 2
			"attackerpass",
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "username does not match authenticated user")

		var fleetErr fleeterror.FleetError
		if assert.ErrorAs(t, err, &fleetErr, "Should be a FleetError") {
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode, "Should be a PermissionDenied error")
		}
	})

	t.Run("passes authentication and proceeds when credentials match session", func(t *testing.T) {
		mockVerifier := &mockCredentialsVerifier{shouldFail: false}
		mockUserStore := &mockUserStoreForAuth{
			users: map[string]interfaces.User{
				"testuser": {ID: 1, Username: "testuser"},
			},
		}

		service := &Service{
			credentialsVerifier: mockVerifier,
			userStore:           mockUserStore,
		}

		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID:         1,
			OrganizationID: 100,
		})

		// nil pools cause a "default pool is required" error, proving auth passed.
		_, err := service.UpdateMiningPools(ctx, nil, nil, nil, nil, "testuser", "validpass")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "default pool is required")

		var fleetErr fleeterror.FleetError
		if assert.ErrorAs(t, err, &fleetErr, "Should be a FleetError") {
			assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode, "Should fail on missing pool, not credentials")
		}
	})

	t.Run("rejects update when credentials are empty", func(t *testing.T) {
		service := &Service{
			credentialsVerifier: &mockCredentialsVerifier{shouldFail: false},
			userStore:           &mockUserStoreForAuth{users: map[string]interfaces.User{}},
		}

		ctx := authn.SetInfo(context.Background(), &session.Info{
			UserID:         1,
			OrganizationID: 100,
		})

		_, err := service.UpdateMiningPools(ctx, nil, nil, nil, nil, "", "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "user_username is required")

		var fleetErr fleeterror.FleetError
		if assert.ErrorAs(t, err, &fleetErr, "Should be a FleetError") {
			assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode, "Should be an InvalidArgument error")
		}
	})
}
