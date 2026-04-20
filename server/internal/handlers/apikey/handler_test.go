package apikey_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"

	apikeyv1 "github.com/block/proto-fleet/server/generated/grpc/apikey/v1"
	authv1 "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	db2 "github.com/block/proto-fleet/server/internal/infrastructure/db"
	id "github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestApiKeyHandler(t *testing.T) {
	testConfig, err := testutil.GetTestConfig()
	assert.NoError(t, err)

	t.Run("full CRUD: create, list, revoke, list", func(t *testing.T) {
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infra := testutil.NewInfrastructureProvider(t, serviceProvider, interceptors.UnauthenticatedProcedures)

		testUser := databaseService.CreateSuperAdminUser()

		// Authenticate to get session cookie
		authResp, err := infra.AuthClient.Authenticate(t.Context(), connect.NewRequest(&authv1.AuthenticateRequest{
			Username: testUser.Username,
			Password: testUser.Password,
		}))
		assert.NoError(t, err)
		sessionCookie := authResp.Header().Get("Set-Cookie")

		// Create API key
		createReq := connect.NewRequest(&apikeyv1.CreateApiKeyRequest{
			Name: "my-ci-key",
		})
		createReq.Header().Set("Cookie", sessionCookie)

		createResp, err := infra.ApiKeyClient.CreateApiKey(t.Context(), createReq)
		assert.NoError(t, err)
		assert.NotEqual(t, "", createResp.Msg.ApiKey)
		assert.Equal(t, "my-ci-key", createResp.Msg.Info.Name)
		assert.NotEqual(t, "", createResp.Msg.Info.KeyId)
		assert.NotEqual(t, "", createResp.Msg.Info.Prefix)
		assert.Equal(t, testUser.Username, createResp.Msg.Info.CreatedBy)

		keyID := createResp.Msg.Info.KeyId

		// List — should contain the key
		listReq := connect.NewRequest(&apikeyv1.ListApiKeysRequest{})
		listReq.Header().Set("Cookie", sessionCookie)

		listResp, err := infra.ApiKeyClient.ListApiKeys(t.Context(), listReq)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(listResp.Msg.ApiKeys))
		assert.Equal(t, "my-ci-key", listResp.Msg.ApiKeys[0].Name)

		// Revoke the key
		revokeReq := connect.NewRequest(&apikeyv1.RevokeApiKeyRequest{
			KeyId: keyID,
		})
		revokeReq.Header().Set("Cookie", sessionCookie)

		_, err = infra.ApiKeyClient.RevokeApiKey(t.Context(), revokeReq)
		assert.NoError(t, err)

		// List again — should be empty
		listReq2 := connect.NewRequest(&apikeyv1.ListApiKeysRequest{})
		listReq2.Header().Set("Cookie", sessionCookie)

		listResp2, err := infra.ApiKeyClient.ListApiKeys(t.Context(), listReq2)
		assert.NoError(t, err)
		assert.Equal(t, 0, len(listResp2.Msg.ApiKeys))
	})

	t.Run("revoke non-existent key returns not found", func(t *testing.T) {
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infra := testutil.NewInfrastructureProvider(t, serviceProvider, interceptors.UnauthenticatedProcedures)

		testUser := databaseService.CreateSuperAdminUser()

		authResp, err := infra.AuthClient.Authenticate(t.Context(), connect.NewRequest(&authv1.AuthenticateRequest{
			Username: testUser.Username,
			Password: testUser.Password,
		}))
		assert.NoError(t, err)
		sessionCookie := authResp.Header().Get("Set-Cookie")

		revokeReq := connect.NewRequest(&apikeyv1.RevokeApiKeyRequest{
			KeyId: "nonexistent-key-id",
		})
		revokeReq.Header().Set("Cookie", sessionCookie)

		_, err = infra.ApiKeyClient.RevokeApiKey(t.Context(), revokeReq)
		assert.Error(t, err)
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("API key auth is rejected for API key lifecycle endpoints", func(t *testing.T) {
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infra := testutil.NewInfrastructureProvider(t, serviceProvider, interceptors.UnauthenticatedProcedures)

		testUser := databaseService.CreateSuperAdminUser()

		fullKey, _, err := serviceProvider.ApiKeyService.Create(
			t.Context(), testUser.DatabaseID, testUser.OrganizationID,
			"ext-id", testUser.Username, "test-key", nil,
		)
		assert.NoError(t, err)

		// API key should NOT be able to create new keys (self-replication prevention)
		createReq := connect.NewRequest(&apikeyv1.CreateApiKeyRequest{Name: "replicated-key"})
		createReq.Header().Set("Authorization", "Bearer "+fullKey)
		_, err = infra.ApiKeyClient.CreateApiKey(t.Context(), createReq)
		assert.Error(t, err)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

		// API key should NOT be able to list keys
		listReq := connect.NewRequest(&apikeyv1.ListApiKeysRequest{})
		listReq.Header().Set("Authorization", "Bearer "+fullKey)
		_, err = infra.ApiKeyClient.ListApiKeys(t.Context(), listReq)
		assert.Error(t, err)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

		// API key should NOT be able to revoke keys
		revokeReq := connect.NewRequest(&apikeyv1.RevokeApiKeyRequest{KeyId: "any"})
		revokeReq.Header().Set("Authorization", "Bearer "+fullKey)
		_, err = infra.ApiKeyClient.RevokeApiKey(t.Context(), revokeReq)
		assert.Error(t, err)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})

	t.Run("logout via API key returns FailedPrecondition", func(t *testing.T) {
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infra := testutil.NewInfrastructureProvider(t, serviceProvider, interceptors.UnauthenticatedProcedures)

		testUser := databaseService.CreateSuperAdminUser()

		fullKey, _, err := serviceProvider.ApiKeyService.Create(
			t.Context(), testUser.DatabaseID, testUser.OrganizationID,
			"ext-id", testUser.Username, "test-key", nil,
		)
		assert.NoError(t, err)

		logoutReq := connect.NewRequest(&authv1.LogoutRequest{})
		logoutReq.Header().Set("Authorization", "Bearer "+fullKey)

		_, err = infra.AuthClient.Logout(t.Context(), logoutReq)
		assert.Error(t, err)
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "not supported for API key")
	})

	t.Run("reject expired API key on creation", func(t *testing.T) {
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)

		testUser := databaseService.CreateSuperAdminUser()

		pastTime := time.Now().Add(-1 * time.Hour)
		_, _, err := serviceProvider.ApiKeyService.Create(
			t.Context(), testUser.DatabaseID, testUser.OrganizationID,
			"ext-id", testUser.Username, "expired-key", &pastTime,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "future")
	})

	t.Run("non-admin user is denied access to API key management", func(t *testing.T) {
		databaseService := testutil.NewDatabaseService(t, testConfig)
		serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)
		infra := testutil.NewInfrastructureProvider(t, serviceProvider, interceptors.UnauthenticatedProcedures)

		// Create a super admin first (to get an org)
		adminUser := databaseService.CreateSuperAdminUser()

		// Create a non-admin user in the same org with a VIEWER role
		viewerPassword := "viewerpass"
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(viewerPassword), bcrypt.DefaultCost)
		assert.NoError(t, err)

		var viewerDBID int64
		err = db2.WithTransactionNoResult(context.Background(), databaseService.DB, func(q *sqlc.Queries) error {
			userID, err := q.CreateUser(context.Background(), sqlc.CreateUserParams{
				UserID:       id.GenerateID(),
				Username:     "viewer@example.com",
				PasswordHash: string(hashedPassword),
				CreatedAt:    time.Now(),
			})
			if err != nil {
				return err
			}
			viewerDBID = userID

			roleID, err := q.UpsertRole(context.Background(), sqlc.UpsertRoleParams{
				Name:        "VIEWER",
				Description: sql.NullString{String: "Read-only role", Valid: true},
			})
			if err != nil {
				return err
			}

			return q.CreateUserOrganization(context.Background(), sqlc.CreateUserOrganizationParams{
				UserID:         userID,
				RoleID:         roleID,
				OrganizationID: adminUser.OrganizationID,
			})
		})
		assert.NoError(t, err)
		_ = viewerDBID

		// Authenticate as the viewer
		authResp, err := infra.AuthClient.Authenticate(t.Context(), connect.NewRequest(&authv1.AuthenticateRequest{
			Username: "viewer@example.com",
			Password: viewerPassword,
		}))
		assert.NoError(t, err)
		sessionCookie := authResp.Header().Get("Set-Cookie")

		// Attempt to create an API key — should be denied
		createReq := connect.NewRequest(&apikeyv1.CreateApiKeyRequest{Name: "viewer-key"})
		createReq.Header().Set("Cookie", sessionCookie)
		_, err = infra.ApiKeyClient.CreateApiKey(t.Context(), createReq)
		assert.Error(t, err)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

		// Attempt to list API keys — should be denied
		listReq := connect.NewRequest(&apikeyv1.ListApiKeysRequest{})
		listReq.Header().Set("Cookie", sessionCookie)
		_, err = infra.ApiKeyClient.ListApiKeys(t.Context(), listReq)
		assert.Error(t, err)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

		// Attempt to revoke an API key — should be denied
		revokeReq := connect.NewRequest(&apikeyv1.RevokeApiKeyRequest{KeyId: "any-key-id"})
		revokeReq.Header().Set("Cookie", sessionCookie)
		_, err = infra.ApiKeyClient.RevokeApiKey(t.Context(), revokeReq)
		assert.Error(t, err)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})
}
