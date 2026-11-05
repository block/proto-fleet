package hostedaccounts_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/session"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func workerBatchFixture(t *testing.T, count int) (poolStepUpFixture, *poolconfigurations.Service, context.Context, []poolconfigurations.Review) {
	t.Helper()
	f := newPoolStepUpFixture(t)
	f.info.SessionID = uuid.NewString()
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx := f.sessionContext(t)
	q := sqlc.New(f.db)
	role, err := q.GetBuiltinRoleForOrg(ctx, sqlc.GetBuiltinRoleForOrgParams{OrganizationID: sql.NullInt64{Int64: f.org, Valid: true}, BuiltinKey: sql.NullString{String: "SUPER_ADMIN", Valid: true}})
	require.NoError(t, err)
	_, err = q.AssignRole(ctx, sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: role.ID, ScopeType: "org"})
	require.NoError(t, err)
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer)
	names := map[string]string{}
	for i := range count {
		config, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: fmt.Sprintf("Config %d", i), Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: fmt.Sprintf("payout.%d", i), WorkerTemplate: "{miner_name}"}}})
		require.NoError(t, err)
		miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		_, err = q.AdoptPoolConfigurationAssignment(ctx, sqlc.AdoptPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: "device", TargetID: miner, DeviceID: miner, ConfigurationID: config.ID, RevisionID: config.RevisionID})
		require.NoError(t, err)
		var identifier string
		require.NoError(t, f.db.QueryRowContext(ctx, `SELECT device_identifier FROM device WHERE id=$1`, miner).Scan(&identifier))
		names[identifier] = fmt.Sprintf("worker-%d", i)
	}
	reviews, err := service.ReviewWorkerNames(ctx, names)
	require.NoError(t, err)
	require.Len(t, reviews, count)
	return f, service, ctx, reviews
}

func TestPoolWorkerBatchUsesOnePasswordCheckBeyondTenConfigurations(t *testing.T) {
	f, service, ctx, reviews := workerBatchFixture(t, 12)
	// The opening authentication dialog also uses the real password limiter.
	require.NoError(t, f.verifier.VerifySessionCredentials(ctx, f.info.Username, "actor-password"))
	ids := make([]string, len(reviews))
	for i, review := range reviews {
		ids[i] = review.ID
	}
	require.NoError(t, service.AuthorizeWorkerNameReviews(ctx, ids, "actor-password"))
	for _, review := range reviews {
		saved, err := service.ConfirmReview(ctx, "", review.ID, "")
		require.NoError(t, err)
		require.NotNil(t, saved.Operation)
	}
	require.Equal(t, 2, f.verifier.calls, "opening authentication plus one batch approval")
	var count int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation`).Scan(&count))
	require.Equal(t, 12, count)
}

func TestPoolWorkerBatchApprovalIsLimitedToExactReviews(t *testing.T) {
	f, service, ctx, reviews := workerBatchFixture(t, 2)
	require.NoError(t, service.AuthorizeWorkerNameReviews(ctx, []string{reviews[0].ID}, "actor-password"))
	f.requireNoPoolOperations(t)
	_, err := service.ConfirmReview(ctx, "", reviews[1].ID, "")
	require.Error(t, err, "a different review must not borrow approval")
	f.requireNoPoolOperations(t)
	saved, err := service.ConfirmReview(ctx, "", reviews[0].ID, "")
	require.NoError(t, err)
	replay, err := service.ConfirmReview(ctx, "", reviews[0].ID, "")
	require.NoError(t, err)
	require.Equal(t, saved.Operation.ID, replay.Operation.ID)
	require.Equal(t, 1, f.verifier.calls)
}

func TestPoolWorkerBatchApprovalRechecksSessionCredentialAuthorityAndBinding(t *testing.T) {
	for _, change := range []string{"session", "api key", "actor", "password", "username", "permission", "binding", "maintenance"} {
		t.Run(change, func(t *testing.T) {
			f, service, ctx, reviews := workerBatchFixture(t, 1)
			require.NoError(t, service.AuthorizeWorkerNameReviews(ctx, []string{reviews[0].ID}, "actor-password"))
			var err error
			info := f.info
			switch change {
			case "session":
				info.SessionID = uuid.NewString()
				ctx = authn.SetInfo(t.Context(), &info)
			case "api key":
				info.AuthMethod = session.AuthMethodAPIKey
				ctx = authn.SetInfo(t.Context(), &info)
			case "actor":
				info.UserID = f.host
				ctx = authn.SetInfo(t.Context(), &info)
			case "password":
				_, err = f.db.ExecContext(ctx, `UPDATE "user" SET password_hash='changed' WHERE id=$1`, f.user)
			case "username":
				_, err = f.db.ExecContext(ctx, `UPDATE "user" SET username='changed' WHERE id=$1`, f.user)
			case "permission":
				_, err = f.db.ExecContext(ctx, `UPDATE user_organization_role SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2`, f.user, f.org)
			case "binding":
				_, err = f.db.ExecContext(ctx, `UPDATE device SET worker_name='intervening-name' WHERE device_identifier=$1`, reviews[0].WorkerNames[0].DeviceIdentifier)
			case "maintenance":
				_, err = f.db.ExecContext(ctx, `INSERT INTO pool_configuration_cutover(organization_id) VALUES($1)`, f.org)
			}
			require.NoError(t, err)
			_, err = service.ConfirmReview(ctx, "", reviews[0].ID, "")
			require.Error(t, err)
			f.requireNoPoolOperations(t)
			require.Equal(t, 1, f.verifier.calls, "confirmation cannot spend the password budget again")
		})
	}
}

func TestPoolWorkerBatchAuthorizationRejectsInvalidBatchesBeforePasswordVerification(t *testing.T) {
	f, service, ctx, reviews := workerBatchFixture(t, 1)
	for _, ids := range [][]string{nil, {"invalid"}, {reviews[0].ID, reviews[0].ID}, {reviews[0].ID, uuid.NewString()}} {
		require.Error(t, service.AuthorizeWorkerNameReviews(ctx, ids, "actor-password"))
	}
	// A library edit is not a worker-name review, even for the same actor.
	config := reviews[0].Configuration
	edit, err := service.ReviewEdit(ctx, "", config.ID, config.Version, poolconfigurations.RevisionOptions{Name: "Renamed", Slots: []poolconfigurations.SlotOptions{{ID: config.Slots[0].ID, URL: config.Slots[0].URL, Username: config.Slots[0].Username, WorkerTemplate: config.Slots[0].WorkerTemplate}}})
	require.NoError(t, err)
	require.Error(t, service.AuthorizeWorkerNameReviews(ctx, []string{reviews[0].ID, edit.ID}, "actor-password"))
	require.Zero(t, f.verifier.calls)
	var grants int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_review_authorization`).Scan(&grants))
	require.Zero(t, grants)
	f.requireNoPoolOperations(t)
}

func TestPoolWorkerBatchAuthorizationRejectsCredentialRacesAtomically(t *testing.T) {
	for _, change := range []string{"wrong password", "password reset", "permission revoked"} {
		t.Run(change, func(t *testing.T) {
			f, service, ctx, reviews := workerBatchFixture(t, 2)
			password := "actor-password"
			if change == "wrong password" {
				password = "wrong"
			} else {
				f.verifier.afterVerification = func() error {
					var err error
					if change == "password reset" {
						_, err = f.db.ExecContext(ctx, `UPDATE "user" SET password_hash='changed' WHERE id=$1`, f.user)
					} else {
						_, err = f.db.ExecContext(ctx, `UPDATE user_organization_role SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2`, f.user, f.org)
					}
					if err != nil {
						return fmt.Errorf("mutate batch authorization fixture: %w", err)
					}
					return nil
				}
			}
			require.Error(t, service.AuthorizeWorkerNameReviews(ctx, []string{reviews[0].ID, reviews[1].ID}, password))
			var grants int
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_review_authorization`).Scan(&grants))
			require.Zero(t, grants)
			f.requireNoPoolOperations(t)
		})
	}
}

func TestPoolWorkerBatchApprovalCannotOutliveItsReview(t *testing.T) {
	f, service, ctx, reviews := workerBatchFixture(t, 1)
	require.NoError(t, service.AuthorizeWorkerNameReviews(ctx, []string{reviews[0].ID}, "actor-password"))
	// Review deadlines are immutable. Clone an expired snapshot with the same
	// approval to exercise expiry without sleeping or rewriting a live review.
	expired := uuid.NewString()
	_, err := f.db.ExecContext(ctx, `INSERT INTO pool_configuration_review(review_id,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,expires_at)
 SELECT $1,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,clock_timestamp()-interval '1 second' FROM pool_configuration_review WHERE review_id=$2`, expired, reviews[0].ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(ctx, `INSERT INTO pool_configuration_review_authorization(review_id,session_hash,credential_fingerprint) SELECT $1,session_hash,credential_fingerprint FROM pool_configuration_review_authorization WHERE review_id=$2`, expired, reviews[0].ID)
	require.NoError(t, err)
	_, err = service.ConfirmReview(ctx, "", expired, "")
	var terminal fleeterror.FleetError
	require.ErrorAs(t, err, &terminal)
	require.Equal(t, fleeterror.ErrorCodeTypeEndpoint, terminal.FleetErrorCodeType)
	require.EqualValues(t, pb.ConfirmReviewErrorCode_CONFIRM_REVIEW_ERROR_CODE_EXPIRED_UNACCEPTED, terminal.FleetErrorCode)
	require.Error(t, service.AuthorizeWorkerNameReviews(ctx, []string{expired}, "actor-password"))
	require.Equal(t, 1, f.verifier.calls)
	f.requireNoPoolOperations(t)
}

func TestPoolWorkerMatchingNameRetryUsesCanonicalBatchConfirmation(t *testing.T) {
	f, service, ctx, reviews := workerBatchFixture(t, 1)
	require.NoError(t, service.AuthorizeWorkerNameReviews(ctx, []string{reviews[0].ID}, "actor-password"))
	first, err := service.ConfirmReview(ctx, "", reviews[0].ID, "")
	require.NoError(t, err)
	_, err = f.db.ExecContext(ctx, `UPDATE pool_configuration_operation_target SET state='failed',result_code='interrupted',updated_at=clock_timestamp() WHERE operation_id=$1`, first.Operation.ID)
	require.NoError(t, err)
	change := reviews[0].WorkerNames[0]
	reviews, err = service.ReviewWorkerNames(ctx, map[string]string{change.DeviceIdentifier: change.After})
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	require.Equal(t, change.After, reviews[0].WorkerNames[0].Before)
	require.NoError(t, service.AuthorizeWorkerNameReviews(ctx, []string{reviews[0].ID}, "actor-password"))
	retried, err := service.ConfirmReview(ctx, "", reviews[0].ID, "")
	require.NoError(t, err)
	require.NotEqual(t, first.Operation.ID, retried.Operation.ID)
	require.Equal(t, first.Configuration.RevisionID, retried.Configuration.RevisionID)
	require.Equal(t, "pending", retried.Operation.Targets[0].State)
}
