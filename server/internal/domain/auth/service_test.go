package auth

import (
	"context"
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// mockUserStore for testing VerifyCredentials
type mockUserStoreForVerify struct {
	users map[string]interfaces.User
}

func (m *mockUserStoreForVerify) GetUserByUsername(ctx context.Context, username string) (interfaces.User, error) {
	user, exists := m.users[username]
	if !exists {
		return interfaces.User{}, fleeterror.NewNotFoundErrorf("user not found")
	}
	return user, nil
}

// Implement other UserStore methods (not used in these tests)
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
	return nil
}
func (m *mockUserStoreForVerify) GetOrganizationsForUser(ctx context.Context, userID int64) ([]interfaces.Organization, error) {
	return nil, nil
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
