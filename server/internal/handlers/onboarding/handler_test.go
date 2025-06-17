package onboarding_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/btc-mining/proto-fleet/server/internal/testutil"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"
	"github.com/google/uuid"

	onboardingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
)

func TestHandler_CreateAdminLogin(t *testing.T) {
	t.Run("should create an admin user", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)

		// Make request
		username := "alice@example.com"
		req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
			Username: username,
			Password: "fizzbuzz",
		})

		resp, err := testContext.InfrastructureProvider.OnboardingClient.CreateAdminLogin(t.Context(), req)
		assert.NoError(t, err)

		// Verify response
		assert.NotEqual(t, "", resp.Msg.UserId, "expected userId in response, got nil")
		assert.NoError(t, uuid.Validate(resp.Msg.UserId), "expected userId to be a valid uuid")

		err = assertRoleAndOrgCreated(t, testContext.DatabaseService.DB, username)
		assert.NoError(t, err)
	})

	t.Run("should fail on create an admin user when username not set", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		// Make request
		req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
			Username: "alice@example.com",
			Password: "",
		})

		_, err := testContext.InfrastructureProvider.OnboardingClient.CreateAdminLogin(t.Context(), req)
		assert.Error(t, err)

	})

	t.Run("should fail on create an admin user when password not set", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)

		// Make request
		req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
			Username: "",
			Password: "fizzbuzz",
		})

		_, err := testContext.InfrastructureProvider.OnboardingClient.CreateAdminLogin(t.Context(), req)
		assert.Error(t, err)
	})
}

func assertRoleAndOrgCreated(t *testing.T, conn *sql.DB, username string) error {
	return db.WithTransactionNoResult(t.Context(), conn, func(q *sqlc.Queries) error {
		dbUser, err := q.GetUserByUsername(t.Context(), username)
		assert.NoError(t, err)
		dbOrgs, err := q.GetOrganizationsForUser(t.Context(), dbUser.ID)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(dbOrgs), "should have only 1 org")
		dbRole, err := q.GetUserRoleInOrganization(t.Context(), sqlc.GetUserRoleInOrganizationParams{
			UserID:         dbUser.ID,
			OrganizationID: dbOrgs[0].ID,
		})
		assert.NoError(t, err)
		assert.Equal(t, "SUPER_ADMIN", dbRole.Name, "should create the SUPER_ADMIN role")
		assert.NotZero(t, dbOrgs[0].OrgID, "should have an org ID")
		assert.True(t, strings.HasPrefix(dbOrgs[0].Name, "Organization "), "should create default org name")
		return nil
	})
}
