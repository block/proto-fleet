package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPoolReadsContinueDuringDeviceWrite(t *testing.T) {
	for _, customer := range []bool{false, true} {
		name := "operator"
		if customer {
			name = "customer"
		}
		t.Run(name, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			actor, accountID := f.host, ""
			scope := poolconfigurations.Scope{OrganizationID: f.org}
			ctx := authn.SetInfo(t.Context(), &session.Info{UserID: f.host, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			if customer {
				actor, accountID, scope.AccountID = f.user, f.account.AccountID, f.account.ID
				var err error
				ctx, err = hostedaccounts.WithRequestAccountID(f.sessionContext(t), accountID)
				require.NoError(t, err)
				require.NoError(t, f.allocate(t, f.host, f.root, accountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			}
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			repo := poolconfigurations.NewRepository(cipher)
			service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer)
			options := poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}}
			config, err := service.Create(ctx, accountID, options)
			require.NoError(t, err)
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
			review, err := service.ReviewAssignment(ctx, accountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
			require.NoError(t, err)
			saved, err := db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
				return repo.ConfirmReview(ctx, q, scope, actor, review.ID, reviewer)
			})
			require.NoError(t, err)
			started, resume := make(chan struct{}), make(chan struct{})
			device := &poolDevice{onWrite: func() { close(started); <-resume }}
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
			workCtx, cancelWork := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancelWork()
			done := make(chan error, 1)
			go func() { done <- worker.Execute(workCtx, f.org, saved.Operation.ID, target) }()
			defer func() { close(resume); require.NoError(t, <-done) }()
			select {
			case <-started:
			case <-workCtx.Done():
				t.Fatal("worker did not reach device write")
			}
			readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			operation, err := service.GetOperation(readCtx, accountID, saved.Operation.ID)
			require.NoError(t, err, "status reads must not wait for hardware I/O")
			require.Equal(t, "applying", operation.Targets[0].State)
			_, err = service.ListOperations(readCtx, accountID, config.ID, "")
			require.NoError(t, err)
			_, err = service.Get(readCtx, accountID, config.ID)
			require.NoError(t, err)
			_, err = service.List(readCtx, accountID, "", 50, false)
			require.NoError(t, err)
			writeCtx, cancelWrite := context.WithTimeout(ctx, 200*time.Millisecond)
			defer cancelWrite()
			_, err = service.Create(writeCtx, accountID, options)
			require.Error(t, err, "management must retain its conflicting organization lock")
			require.ErrorIs(t, writeCtx.Err(), context.DeadlineExceeded)
		})
	}
}

func TestPoolReviewRequiresNativeStratumV2(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	pools, _, _, _, ctx := poolFixture(t, f)
	ctx, err := hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
	require.NoError(t, err)
	const v2 = "stratum2+tcp://pool.example:3333/79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
	for _, scenario := range []struct {
		name            string
		urls            []string
		native, allowed bool
	}{
		{"v1 without native support", []string{"stratum+tcp://pool.example"}, false, true},
		{"v2 primary unsupported", []string{v2}, false, false},
		{"v2 backup unsupported", []string{"stratum+tcp://pool.example", v2}, false, false},
		{"v2 supported", []string{v2}, true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			reviewer := poolapplications.NewReviewer(pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true, sdk.CapabilityNativeStratumV2: scenario.native})
			slots := make([]poolconfigurations.Endpoint, len(scenario.urls))
			for i, url := range scenario.urls {
				slots[i] = poolconfigurations.Endpoint{URL: url, Username: "payout"}
			}
			err := db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
				_, err := reviewer.ReviewTargets(ctx, q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}, f.user, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}}, slots)
				return err
			})
			if scenario.allowed {
				require.NoError(t, err)
			} else {
				require.True(t, fleeterror.IsFailedPreconditionError(err), "expected unsupported V2 rejection, got %v", err)
			}
		})
	}
}
