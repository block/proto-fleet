package mininggroups

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/session"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAuthorizedOperatorCanResumeDeactivationAfterLostReplyAndRevocation(t *testing.T) {
	ctx, f := deactivationFixture(t)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	var nextUser int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO "user"(user_id,username,password_hash)
 SELECT gen_random_uuid()::text,gen_random_uuid()::text,password_hash FROM "user" WHERE id=$1 RETURNING id`, info.UserID).Scan(&nextUser))
	_, err = f.conn.ExecContext(ctx, `INSERT INTO user_organization(user_id,organization_id,role_id) SELECT $1,organization_id,role_id FROM user_organization WHERE user_id=$2 AND organization_id=$3 AND deleted_at IS NULL`, nextUser, info.UserID, info.OrganizationID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO user_organization_role(user_id,organization_id,role_id,scope_type,scope_id) SELECT $1,organization_id,role_id,scope_type,scope_id FROM user_organization_role WHERE user_id=$2 AND organization_id=$3 AND deleted_at IS NULL`, nextUser, info.UserID, info.OrganizationID)
	require.NoError(t, err)
	nextCtx := authn.SetInfo(ctx, &session.Info{UserID: nextUser, OrganizationID: info.OrganizationID, AuthMethod: session.AuthMethodSession})
	requestID := uuid.NewString()
	_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, requestID)
	require.NoError(t, err)
	f.reconcile(t, ctx)
	prepared := f.state(t)
	f.miner.loseDeactivate = true
	f.reconcile(t, ctx)
	require.False(t, f.miner.enabled)
	require.True(t, f.state(t).Step.Valid)
	setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerStartMining, authz.PermMinerStopMining})
	f.miner.calls = nil
	f.reconcile(t, ctx)
	blocked := f.state(t)
	require.Equal(t, "blocked", blocked.Status)
	require.Empty(t, f.miner.calls, "revocation cannot authorize a replay")
	_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, requestID)
	require.ErrorContains(t, err, "permission")
	require.Equal(t, blocked, f.state(t))
	_, err = f.service.Deactivate(nextCtx, f.target.DeviceIdentifier, uuid.NewString())
	require.Error(t, err, "recovery must reuse the saved request")
	_, err = f.conn.ExecContext(ctx, `CREATE FUNCTION reject_deactivation_resume_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'resume commit unavailable'; END $$;
 CREATE CONSTRAINT TRIGGER reject_deactivation_resume_commit AFTER UPDATE ON container_mining_group_state DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.status='applying') EXECUTE FUNCTION reject_deactivation_resume_commit()`)
	require.NoError(t, err)
	_, err = f.service.Deactivate(nextCtx, f.target.DeviceIdentifier, requestID)
	require.ErrorContains(t, err, "resume commit unavailable")
	require.Equal(t, blocked, f.state(t), "failed recovery commits must preserve the original actor and request")
	_, err = f.conn.ExecContext(ctx, `DROP TRIGGER reject_deactivation_resume_commit ON container_mining_group_state`)
	require.NoError(t, err)
	_, err = f.service.Deactivate(nextCtx, f.target.DeviceIdentifier, requestID)
	require.NoError(t, err)
	resumed := f.state(t)
	require.Equal(t, nextUser, resumed.ActorID.Int64)
	require.Equal(t, prepared.OperationID, resumed.OperationID)
	require.Equal(t, prepared.AttemptID, resumed.AttemptID)
	require.Equal(t, prepared.Step, resumed.Step)
	require.Equal(t, prepared.StepKey, resumed.StepKey)
	require.Empty(t, f.miner.calls, "reauthorization itself must perform no controller write")
	f.reconcile(t, nextCtx)
	require.Equal(t, prepared.StepKey.UUID.String(), f.miner.deactivatedKey)
	f.reconcile(t, nextCtx)
	require.Equal(t, statusComplete, f.state(t).Status)
	var attempts, audits int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND operation_kind='mutation'`, f.target.DeviceID).Scan(&attempts))
	require.Equal(t, 1, attempts)
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM activity_log WHERE event_type='mining_groups.deactivate.resumed' AND user_id=(SELECT user_id FROM "user" WHERE id=$1)`, nextUser).Scan(&audits))
	require.Equal(t, 1, audits)
}

type heldReceiptMiner struct {
	*transferFailureMiner
	entered chan struct{}
	release chan struct{}
}

func (m *heldReceiptMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *heldReceiptMiner) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if request.Action == "operation" {
		close(m.entered)
		select {
		case <-m.release:
		case <-ctx.Done():
			return sdk.MiningGroupResult{}, fmt.Errorf("receipt read canceled: %w", ctx.Err())
		}
	}
	return m.transferFailureMiner.MiningGroups(ctx, request)
}

func TestTransferReceiptPollingDoesNotHoldOrganizationAdmission(t *testing.T) {
	ctx, f, firmware, _, _ := newTransferFailureFixture(t)
	firmware.transferPhase = pendingTransferPhase
	f.reconcile(t, ctx)
	f.reconcile(t, ctx)
	require.NotEmpty(t, f.state(t).ControllerOperationID)
	miner := &heldReceiptMiner{transferFailureMiner: firmware, entered: make(chan struct{}), release: make(chan struct{})}
	f.service.miners = miner
	release := sync.OnceFunc(func() { close(miner.release) })
	t.Cleanup(release)
	done := make(chan error, 1)
	go func() {
		done <- f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier)
	}()
	select {
	case <-miner.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("receipt polling did not start")
	}
	tx, err := f.conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(ctx, `SELECT id FROM organization WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, f.target.OrganizationID)
	require.NoError(t, err, "read-only receipt polling must leave organization admission available")
	require.NoError(t, tx.Rollback())
	release()
	require.NoError(t, <-done)
}
