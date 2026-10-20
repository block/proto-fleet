package hostedaccounts_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

// Hold telemetry admission after report authority has been checked, and observe
// PostgreSQL's actual wait graph instead of relying on query duration.
func blockMiningProjection(t *testing.T, conn *sql.DB) (*sql.Tx, int) {
	t.Helper()
	tx, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(t.Context(), "LOCK TABLE hosted_account_asset IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)
	var pid int
	require.NoError(t, tx.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&pid))
	return tx, pid
}

func waitForMiningProjections(t *testing.T, conn *sql.DB, blocker, count int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting int
		err := conn.QueryRowContext(t.Context(), `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND $1 = ANY(pg_blocking_pids(pid))
			AND (query LIKE '-- name: ListHostedAccountMiningAsset%'
			 OR query LIKE '-- name: ListHostedAccountAssetFilterOptions%')`, blocker).Scan(&waiting)
		return err == nil && waiting == count
	}, 2*time.Second, 10*time.Millisecond)
}

func waitForReportBlockedMutation(t *testing.T, conn *sql.DB, query string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var blocked bool
		err := conn.QueryRowContext(t.Context(), `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity mutation
			JOIN pg_stat_activity report ON report.pid = ANY(pg_blocking_pids(mutation.pid))
			WHERE mutation.datname = current_database() AND mutation.query LIKE $1
			AND (report.query LIKE '-- name: ListHostedAccountMiningAsset%'
			 OR report.query LIKE '-- name: ListHostedAccountAssetFilterOptions%')
		)`, query).Scan(&blocked)
		return err == nil && blocked
	}, 2*time.Second, 10*time.Millisecond)
}

func TestMiningReportsOverlapAndExcludeAssignments(t *testing.T) {
	for _, scope := range []string{"host", "customer"} {
		t.Run(scope, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			actor, account := f.host, f.root
			if scope == "customer" {
				actor, account = f.user, client.AccountID
			}
			barrier, blocker := blockMiningProjection(t, f.db)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			reports := make(chan error, 2)
			go func() {
				_, err := f.svc.MiningSummary(ctx, actor, f.org, account)
				reports <- err
			}()
			waitForMiningProjections(t, f.db, blocker, 1)
			go func() {
				_, err := f.svc.ListAssets(ctx, actor, f.org, &pb.ListAccountAssetsRequest{AccountId: account})
				reports <- err
			}()
			// Same-actor readers must both reach telemetry before either finishes.
			// For host reports this also exercises both root and physical RBAC checks.
			waitForMiningProjections(t, f.db, blocker, 2)
			assignment := make(chan error, 1)
			go func() {
				assignment <- f.svc.AssignAssets(ctx, f.host, f.org, &pb.AssignAccountAssetsRequest{
					AccountId: f.root, ChildAccountId: client.AccountID,
					Intent: pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN,
					Assets: []*pb.MiningAssetSelection{selected(pb.MiningAssetKind_MINING_ASSET_KIND_MINER, miner, 0)},
				})
			}()
			waitForReportBlockedMutation(t, f.db, "-- name: LockHostedAccountOrganization :one%")
			require.NoError(t, barrier.Commit())
			for range 2 {
				require.NoError(t, <-reports)
			}
			require.NoError(t, <-assignment)
		})
	}
}

func TestMiningReportSharedAuthorityExcludesRoleRevocation(t *testing.T) {
	f := setup(t)
	assignments, err := sqlc.New(f.db).ListAssignmentsForUser(t.Context(), sqlc.ListAssignmentsForUserParams{UserID: f.host, OrganizationID: f.org})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	barrier, blocker := blockMiningProjection(t, f.db)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	report := make(chan error, 1)
	go func() {
		_, err := f.svc.MiningSummary(ctx, f.host, f.org, f.root)
		report <- err
	}()
	waitForMiningProjections(t, f.db, blocker, 1)
	revocation := make(chan error, 1)
	go func() { revocation <- sqlc.New(f.db).UnassignRole(ctx, assignments[0].ID) }()
	waitForReportBlockedMutation(t, f.db, "-- name: UnassignRole :exec%")
	require.NoError(t, barrier.Commit())
	require.NoError(t, <-report)
	require.NoError(t, <-revocation)
	_, err = f.svc.MiningSummary(ctx, f.host, f.org, f.root)
	require.True(t, fleeterror.IsForbiddenError(err), "the next report must see revoked authority: %v", err)
}
