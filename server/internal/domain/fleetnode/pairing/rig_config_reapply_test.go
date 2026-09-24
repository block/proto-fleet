package pairing_test

import (
	"context"
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
)

func TestPersistFleetNodePairResult_ReappliesOnlyCommittedPairTargets(t *testing.T) {
	db, orgID, svc, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-targeted-config")
	assignedBy := int64(1)
	ctx := t.Context()

	// An existing rig must not be targeted again as new devices join the fleet.
	upsertNodeDiscovered(t, svc, orgID, nodeID, "existing")
	status, err := svc.PersistFleetNodePairResult(ctx, nodeID, orgID, pairResult("existing", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED), &assignedBy)
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusPaired, status)

	var requests [][]string
	svc.WithRigConfigReapplier(func(ctx context.Context, gotOrgID, userID int64, identifiers []string) {
		require.NoError(t, ctx.Err())
		require.Equal(t, orgID, gotOrgID)
		require.Equal(t, assignedBy, userID)
		require.Len(t, identifiers, 1)
		// Read through a separate connection to prove the bind committed before
		// requesting convergence. A later failure must not undo this request.
		require.True(t, deviceBoundToNode(t, db, orgID, nodeID, identifiers[0]))
		require.Equal(t, fleetnodepairing.StatusPaired, devicePairingStatus(t, db, orgID, identifiers[0]))
		requests = append(requests, append([]string(nil), identifiers...))
	})

	for _, identifier := range []string{"new-proto", "auth-needed", "failed", "rolled-back", "new-bitmain"} {
		upsertNodeDiscovered(t, svc, orgID, nodeID, identifier)
	}
	protoResult := pairResult("new-proto", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED)
	protoResult.Manufacturer = "Proto"
	protoResult.Model = "Proto Rig"
	status, err = svc.PersistFleetNodePairResult(ctx, nodeID, orgID, protoResult, &assignedBy)
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusPaired, status)

	status, err = svc.PersistFleetNodePairResult(ctx, nodeID, orgID, pairResult("auth-needed", gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED), &assignedBy)
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusAuthenticationNeeded, status)
	status, err = svc.PersistFleetNodePairResult(ctx, nodeID, orgID, pairResult("failed", gatewaypb.PairOutcome_PAIR_OUTCOME_ERROR), &assignedBy)
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusFailed, status)

	// This fails after the bind, causing its transaction to roll back.
	invalid := pairResult("rolled-back", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED)
	invalid.EncryptedCredentials = &gatewaypb.EncryptedCredentials{}
	_, err = svc.PersistFleetNodePairResult(ctx, nodeID, orgID, invalid, &assignedBy)
	require.Error(t, err)
	require.False(t, deviceBoundToNode(t, db, orgID, nodeID, "rolled-back"))

	// A stale AUTH_NEEDED report returns PAIRED for an existing binding, but
	// must not cause a configuration request merely because of that status.
	status, err = svc.PersistFleetNodePairResult(ctx, nodeID, orgID, pairResult("existing", gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED), &assignedBy)
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusPaired, status)

	status, err = svc.PersistFleetNodePairResult(ctx, nodeID, orgID, pairResult("new-bitmain", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED), &assignedBy)
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusPaired, status)
	// Eligibility for curtailment is decided by the reconciliation store;
	// pairing must provide precise targets for every successfully bound driver.
	require.Equal(t, [][]string{{"new-proto"}, {"new-bitmain"}}, requests)
}

func TestPairDevice_ReappliesOnlyBoundDeviceAfterCommit(t *testing.T) {
	db, orgID, svc, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-bind-targeted-config")
	assignedBy := int64(1)
	require.NoError(t, svc.PairDevice(t.Context(), nodeID, insertDevice(t, db, orgID), orgID, &assignedBy))
	deviceID := insertDevice(t, db, orgID)
	var identifier string
	require.NoError(t, db.QueryRow(`SELECT device_identifier FROM device WHERE id=$1`, deviceID).Scan(&identifier))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	svc = fleetnodepairing.NewService(
		sqlstores.NewSQLFleetNodePairingStore(db),
		sqlstores.NewSQLFleetNodeEnrollmentStore(db),
		cancelAfterCommitTransactor{Transactor: sqlstores.NewSQLTransactor(db), cancel: cancel},
	)
	var requests [][]string
	svc.WithRigConfigReapplier(func(ctx context.Context, gotOrgID, userID int64, identifiers []string) {
		require.NoError(t, ctx.Err())
		require.Equal(t, orgID, gotOrgID)
		require.Equal(t, assignedBy, userID)
		require.True(t, deviceBoundToNode(t, db, orgID, nodeID, identifier))
		requests = append(requests, identifiers)
	})

	require.NoError(t, svc.PairDevice(ctx, nodeID, deviceID, orgID, &assignedBy))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, [][]string{{identifier}}, requests)
}

type cancelAfterCommitTransactor struct {
	interfaces.Transactor
	cancel context.CancelFunc
}

func (t cancelAfterCommitTransactor) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	err := t.Transactor.RunInTx(ctx, fn)
	if err == nil {
		t.cancel()
	}
	return err
}
