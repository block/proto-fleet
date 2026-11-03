package hostedaccounts_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPoolConfigurationCheckReadsWithoutWritingAndPreservesUncertainty(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
	require.NoError(t, err)
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	repo := poolconfigurations.NewRepository(cipher)
	device := &poolDevice{unknownPassword: true}
	service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer).WithChecker(poolapplications.NewChecker(f.db, reviewer, poolMinerGetter{device}, cipher, f.pools))
	config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
	// No assignment does not imply an empty stack. Read it without creating intent.
	empty, err := service.CheckConfiguration(ctx, f.account.AccountID, "", target)
	require.NoError(t, err)
	require.True(t, empty.ReadbackAvailable)
	require.Empty(t, empty.State)
	// Concurrent observations by the same actor must not upgrade organization
	// locks after taking device bindings (which deadlocked parallel table reads).
	errs := make(chan error, 8)
	var checks sync.WaitGroup
	for range 8 {
		checks.Go(func() { _, err := service.CheckConfiguration(ctx, f.account.AccountID, "", target); errs <- err })
	}
	checks.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	device.pools = []dto.MiningPool{{URL: "stratum+tcp://device.example", Username: "device-worker"}}
	reported, err := service.CheckConfiguration(ctx, f.account.AccountID, "", target)
	require.NoError(t, err)
	require.Equal(t, "device_configuration", reported.State)
	require.Len(t, reported.Reported, 1)
	require.Zero(t, device.writes)
	before, err := service.GetAssignments(ctx, f.account.AccountID, nil, []int64{miner}, nil)
	require.NoError(t, err)
	require.Len(t, before.Assignments, 1)
	require.Empty(t, before.Assignments[0].ConfigurationID)
	review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
	require.NoError(t, err)
	saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, "actor-password")
	require.NoError(t, err)
	require.NoError(t, poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher).Execute(t.Context(), f.org, saved.Operation.ID, target))
	device.different = true
	checked, err := service.CheckConfiguration(ctx, f.account.AccountID, config.ID, target)
	require.NoError(t, err)
	require.Equal(t, "different", checked.State)
	require.Equal(t, "externally-changed", checked.Reported[0].Username)
	assignments, err := service.GetAssignments(ctx, f.account.AccountID, nil, []int64{miner}, nil)
	require.NoError(t, err)
	require.Equal(t, "different", assignments.Assignments[0].State)
	_, err = service.ReviewRetry(ctx, f.account.AccountID, saved.Operation.ID, []poolconfigurations.AssignmentTarget{target})
	require.NoError(t, err, "an acknowledged application with a later mismatch can be reapplied")
	// Later observations do not change the original unverified application.
	operation, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
	require.NoError(t, err)
	require.Equal(t, "applied_unverified", operation.Targets[0].State)
	device.different = false
	checked, err = service.CheckConfiguration(ctx, f.account.AccountID, config.ID, target)
	require.NoError(t, err)
	require.Equal(t, "applied_unverified", checked.State, "matching public settings cannot verify an omitted password")
	device.unknownPassword = false
	checked, err = service.CheckConfiguration(ctx, f.account.AccountID, config.ID, target)
	require.NoError(t, err)
	require.Equal(t, "verified", checked.State)
	require.Equal(t, 1, device.writes)
	// A new assignment must not inherit the old check's verified status.
	review, err = service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
	require.NoError(t, err)
	saved, err = service.ConfirmReview(ctx, f.account.AccountID, review.ID, "actor-password")
	require.NoError(t, err)
	assignments, err = service.GetAssignments(ctx, f.account.AccountID, nil, []int64{miner}, nil)
	require.NoError(t, err)
	require.Equal(t, "pending", assignments.Assignments[0].State)
	_, err = service.CheckConfiguration(ctx, f.account.AccountID, config.ID, target)
	require.ErrorContains(t, err, "still running")
	require.NoError(t, poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher).Execute(t.Context(), f.org, saved.Operation.ID, target))
	writes := devicewrite.NewService(f.db)
	require.Error(t, writes.WithHostAttempt(ctx, f.org, miner, 0, commandtype.UpdateMiningPools, func(ctx context.Context) error {
		return devicewrite.Dispatch(ctx, func(context.Context) error { return devicewrite.UncertainDelivery(errors.New("lost response")) })
	}))
	checked, err = service.CheckConfiguration(ctx, f.account.AccountID, config.ID, target)
	require.NoError(t, err)
	require.Equal(t, "write_uncertain", checked.ResultCode, "matching readback must not clear a possibly unfinished write")
	var unresolved int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND status IN ('running','unknown')`, miner).Scan(&unresolved))
	require.Equal(t, 1, unresolved)
	require.Equal(t, 2, device.writes)
	device.different = true
	checked, err = service.CheckConfiguration(ctx, f.account.AccountID, config.ID, target)
	require.NoError(t, err)
	require.Equal(t, "different", checked.State)
	require.Equal(t, "write_uncertain", checked.ResultCode)
	require.True(t, checked.ReadbackAvailable)
	_, err = service.ReviewRetry(ctx, f.account.AccountID, saved.Operation.ID, []poolconfigurations.AssignmentTarget{target})
	require.Error(t, err, "a known mismatch does not resolve the write restriction")

	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ChangePools, false))
	_, err = service.CheckConfiguration(ctx, f.account.AccountID, config.ID, target)
	require.Error(t, err, "allocation must not grant permission to check private settings")
}
