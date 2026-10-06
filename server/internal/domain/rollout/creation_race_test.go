package rollout

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconciliationRechecksAssignmentAfterScan(t *testing.T) {
	for _, replacement := range []string{"", "fw-2"} {
		name := "cleared"
		if replacement != "" {
			name = "reassigned"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, 1)
			ctx := t.Context()
			f.channel(t, allAtOnce, "miner-0")
			first := f.apply(t, "fw-1")
			f.svc.EnforceTick(ctx)
			f.finishUpdate(t, "miner-0", "1.5.0")
			f.svc.EnforceTick(ctx)
			require.Equal(t, StatusCompleted, f.rollout(t, first.ID).Status)
			f.setReportedVersion(t, "miner-0", "1.0.0")

			// Commit the assignment change after the real reconciliation scan,
			// before its creation transaction. The scan's rows remain stale.
			f.svc.store = &afterReconciliationScanStore{QueryProvider: f.svc.store, afterScan: func() {
				if replacement != "" {
					// The new assignment initially needs no rollout. Drift afterward
					// must reconcile the new generation, never the scanned one.
					f.setReportedVersion(t, "miner-0", "2.0.0")
					_, err := f.conn.ExecContext(ctx, `UPDATE device_firmware_deployment
						SET firmware_checksum = $1, firmware_version = '2.0.0' WHERE device_id = $2`, checksum2, f.deviceIDs["miner-0"])
					require.NoError(t, err)
				}
				started, err := f.svc.ApplyFirmware(ctx, f.orgID, testActor, f.channelID, []Assignment{rigAssignment(replacement)}, nil)
				require.NoError(t, err)
				require.Empty(t, started)
				if replacement != "" {
					f.setReportedVersion(t, "miner-0", "1.0.0")
				}
			}}
			f.svc.startNeededRollouts(ctx)
			latest := f.latestRollout(t)
			if replacement == "" {
				assert.Equal(t, first.ID, latest.ID, "clearing the assignment must not create a stale active rollout")
				assert.Equal(t, StatusCompleted, latest.Status)
				return
			}
			assert.NotEqual(t, first.ID, latest.ID)
			assert.Equal(t, StatusActive, latest.Status)
			assert.Equal(t, checksum2, latest.FirmwareChecksum)
			assert.Equal(t, int64(2), latest.AssignmentGeneration)
			assert.Equal(t, SystemActor, latest.StartedBy)
		})
	}
}

func TestConcurrentFinishedRetryRechecksAssignment(t *testing.T) {
	for _, replacement := range []string{"", "fw-2"} {
		name := "cleared"
		if replacement != "" {
			name = "reassigned"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, 1)
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-1")
			_, _, err := f.svc.CancelRollout(t.Context(), f.orgID, started.ID, byOperator)
			require.NoError(t, err)
			require.Equal(t, StatusCanceled, f.rollout(t, started.ID).Status)
			f.svc.activity = nil
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			apply := func(actor Actor) ([]Rollout, error) {
				return f.svc.ApplyFirmware(ctx, f.orgID, actor, f.channelID, []Assignment{rigAssignment(replacement)}, nil)
			}
			retry := func(actor Actor) ([]Rollout, error) {
				r, err := f.svc.RetryFailedDevices(ctx, f.orgID, started.ID, Mutation{Actor: actor})
				if err != nil {
					return nil, err
				}
				return []Rollout{*r}, nil
			}
			first, second := concurrentAssignmentWrites(t, ctx, f, apply, retry)
			require.NoError(t, first.err)
			require.Error(t, second.err)
			info, ok := ReasonOf(second.err)
			require.True(t, ok)
			assert.Equal(t, ReasonStaleGeneration, info.Reason)
			assert.Empty(t, second.rollouts)
			assert.Equal(t, replacement, f.assignedFirmware(t))
		})
	}
}

// These wrappers run every query against Postgres and inject only the
// scheduling boundary between discovery and transactional reconciliation.
type afterReconciliationScanStore struct {
	QueryProvider
	afterScan func()
}

func (s *afterReconciliationScanStore) GetQueries(ctx context.Context) sqlc.Querier {
	return &afterReconciliationScanQueries{Querier: s.QueryProvider.GetQueries(ctx), afterScan: s.afterScan}
}

type afterReconciliationScanQueries struct {
	sqlc.Querier
	afterScan func()
}

func (q *afterReconciliationScanQueries) ListReleaseChannelFirmwareNeedingRollout(ctx context.Context) ([]sqlc.ListReleaseChannelFirmwareNeedingRolloutRow, error) {
	rows, err := q.Querier.ListReleaseChannelFirmwareNeedingRollout(ctx)
	if err == nil {
		q.afterScan()
	}
	return rows, err
}
