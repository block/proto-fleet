package deployment

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
)

func TestFleetLeaseRecoveryRequest(t *testing.T) {
	request := FleetLeaseRecoveryRequest{
		Apply: true, OldClusterID: 10, NewClusterID: 20, WritersFenced: true,
	}
	require.NoError(t, request.validate())
	require.NoError(t, (FleetLeaseRecoveryRequest{}).validate())

	for name, change := range map[string]func(*FleetLeaseRecoveryRequest){
		"same cluster":      func(r *FleetLeaseRecoveryRequest) { r.NewClusterID = r.OldClusterID },
		"empty old cluster": func(r *FleetLeaseRecoveryRequest) { r.OldClusterID = 0 },
		"empty new cluster": func(r *FleetLeaseRecoveryRequest) { r.NewClusterID = 0 },
		"writers unfenced":  func(r *FleetLeaseRecoveryRequest) { r.WritersFenced = false },
		"missing apply":     func(r *FleetLeaseRecoveryRequest) { r.Apply = false },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := request
			change(&invalid)
			require.Error(t, invalid.validate())
		})
	}
}

type leaseRecoveryQuerier struct {
	sqlc.Querier
	lease       sqlc.GetFleetRuntimeLeaseForRecoveryRow
	getErr      error
	rebindEpoch int64
	rebindErr   error
	gets        int
	rebinds     []sqlc.RebindExpiredFleetRuntimeLeaseParams
}

func (q *leaseRecoveryQuerier) GetFleetRuntimeLeaseForRecovery(context.Context) (sqlc.GetFleetRuntimeLeaseForRecoveryRow, error) {
	q.gets++
	return q.lease, q.getErr
}

func (q *leaseRecoveryQuerier) RebindExpiredFleetRuntimeLease(_ context.Context, params sqlc.RebindExpiredFleetRuntimeLeaseParams) (int64, error) {
	q.rebinds = append(q.rebinds, params)
	return q.rebindEpoch, q.rebindErr
}

func TestInspectOrRebindFleetLease(t *testing.T) {
	apply := FleetLeaseRecoveryRequest{Apply: true, OldClusterID: 10, NewClusterID: 20, WritersFenced: true}
	writer := ha.WriterObservation{
		DCSClusterID: "20", WriterGeneration: 5, LeaderName: "patroni-a",
		ServerAddress: "10.0.0.1", ServerPort: 5432, Timeline: 3,
	}
	expiredOldLease := sqlc.GetFleetRuntimeLeaseForRecoveryRow{
		DcsClusterID: "10", HighestWriterGeneration: 4, LeaseEpoch: 7, Expired: true,
	}
	withLease := func(change func(*sqlc.GetFleetRuntimeLeaseForRecoveryRow)) sqlc.GetFleetRuntimeLeaseForRecoveryRow {
		lease := expiredOldLease
		change(&lease)
		return lease
	}
	baseReport := FleetLeaseRecoveryReport{CurrentClusterID: "20", Writer: "patroni-a", CurrentWriterGeneration: 5}

	for _, tt := range []struct {
		name       string
		request    FleetLeaseRecoveryRequest
		writer     ha.WriterObservation
		querier    leaseRecoveryQuerier
		wantErr    string
		wantReport FleetLeaseRecoveryReport
		wantGets   int
		wantRebind bool
	}{
		{
			name: "writer cluster differs from new cluster", request: apply, querier: leaseRecoveryQuerier{lease: expiredOldLease},
			writer:  ha.WriterObservation{DCSClusterID: "30"},
			wantErr: "does not match --new-cluster-id",
		},
		{
			name: "no lease row", request: apply, writer: writer, querier: leaseRecoveryQuerier{getErr: sql.ErrNoRows},
			wantReport: baseReport, wantGets: 1,
		},
		{
			name: "inspect mode reads without rebinding", request: FleetLeaseRecoveryRequest{}, writer: writer,
			querier: leaseRecoveryQuerier{lease: expiredOldLease}, wantGets: 1,
			wantReport: FleetLeaseRecoveryReport{
				CurrentClusterID: "20", StoredClusterID: "10", Writer: "patroni-a", CurrentWriterGeneration: 5,
				StoredWriterGeneration: 4, LeaseEpoch: 7, LeaseExpired: true,
			},
		},
		{
			name: "same cluster with older writer generation", request: apply, writer: writer, wantGets: 1,
			querier: leaseRecoveryQuerier{lease: withLease(func(l *sqlc.GetFleetRuntimeLeaseForRecoveryRow) {
				l.DcsClusterID, l.HighestWriterGeneration = "20", 6
			})},
			wantErr: "older than the lease",
		},
		{
			name: "same cluster with current writer generation", request: apply, writer: writer, wantGets: 1,
			querier: leaseRecoveryQuerier{lease: withLease(func(l *sqlc.GetFleetRuntimeLeaseForRecoveryRow) {
				l.DcsClusterID, l.HighestWriterGeneration = "20", 5
			})},
			wantReport: FleetLeaseRecoveryReport{
				CurrentClusterID: "20", StoredClusterID: "20", Writer: "patroni-a", CurrentWriterGeneration: 5,
				StoredWriterGeneration: 5, LeaseEpoch: 7, LeaseExpired: true,
			},
		},
		{
			name: "stored cluster differs from old cluster", request: apply, writer: writer, wantGets: 1,
			querier: leaseRecoveryQuerier{lease: withLease(func(l *sqlc.GetFleetRuntimeLeaseForRecoveryRow) {
				l.DcsClusterID = "99"
			})},
			wantErr: "does not match --old-cluster-id",
		},
		{
			name: "lease not expired", request: apply, writer: writer, wantGets: 1,
			querier: leaseRecoveryQuerier{lease: withLease(func(l *sqlc.GetFleetRuntimeLeaseForRecoveryRow) {
				l.Expired = false
			})},
			wantErr: "has not expired",
		},
		{
			name: "rebind matches no row", request: apply, writer: writer, wantGets: 1, wantRebind: true,
			querier: leaseRecoveryQuerier{lease: expiredOldLease, rebindErr: sql.ErrNoRows},
			wantErr: "changed during recovery",
		},
		{
			name: "rebind succeeds", request: apply, writer: writer, wantGets: 1, wantRebind: true,
			querier: leaseRecoveryQuerier{lease: expiredOldLease, rebindEpoch: 7},
			wantReport: FleetLeaseRecoveryReport{
				CurrentClusterID: "20", StoredClusterID: "20", Writer: "patroni-a", CurrentWriterGeneration: 5,
				StoredWriterGeneration: 5, LeaseEpoch: 7, LeaseExpired: true, Rebound: true,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			queries := tt.querier
			report, err := inspectOrRebindFleetLease(t.Context(), &queries, tt.writer, tt.request)

			require.Equal(t, tt.wantGets, queries.gets)
			if tt.wantRebind {
				require.Equal(t, []sqlc.RebindExpiredFleetRuntimeLeaseParams{{
					NewClusterID: "20", WriterGeneration: 5, OldClusterID: "10",
					ServerAddress: "10.0.0.1", ServerPort: 5432, Timeline: 3,
				}}, queries.rebinds)
			} else {
				require.Empty(t, queries.rebinds)
			}
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantReport, report)
		})
	}
}

type leaseRecoveryObserverFunc func(context.Context, func(context.Context, ha.WriterObservation) error) (ha.WriterObservation, error)

func (observe leaseRecoveryObserverFunc) ObserveAndRun(
	ctx context.Context,
	action func(context.Context, ha.WriterObservation) error,
) (ha.WriterObservation, error) {
	return observe(ctx, action)
}

func TestRecoverFleetLeaseTransaction(t *testing.T) {
	closingErr := errors.New("etcd leader changed during recovery")
	for _, tt := range []struct {
		name       string
		proofTime  time.Duration
		closingErr error
		wantErr    string
	}{
		{name: "closing proof fails", closingErr: closingErr, wantErr: closingErr.Error()},
		{name: "proof within commit margin", proofTime: leaseCommitMargin / 2, wantErr: "leader proof expired"},
		{name: "valid closing proof", proofTime: time.Minute},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			conn := dbtest.GetTestDB(t)
			queries := sqlc.New(conn)
			identity, err := queries.GetConnectedPostgresIdentity(ctx)
			require.NoError(t, err)
			source := ha.WriterObservation{
				DCSClusterID: "10", WriterGeneration: 41,
				ServerAddress: identity.ServerAddress, ServerPort: identity.ServerPort, Timeline: identity.Timeline,
			}
			store := ha.NewLeaseStore(queries)
			old, err := store.Acquire(ctx, source, uuid.New(), time.Minute)
			require.NoError(t, err)
			require.NoError(t, store.Release(ctx, old))

			replacement := source
			replacement.DCSClusterID = "20"
			replacement.WriterGeneration = 3
			request := FleetLeaseRecoveryRequest{Apply: true, OldClusterID: 10, NewClusterID: 20, WritersFenced: true}
			report, err := recoverFleetLeaseTransaction(ctx, conn, request, func(txQueries sqlc.Querier) (leaseRecoveryObserver, func(), error) {
				observer := leaseRecoveryObserverFunc(func(ctx context.Context, action func(context.Context, ha.WriterObservation) error) (ha.WriterObservation, error) {
					if err := action(ctx, replacement); err != nil {
						return ha.WriterObservation{}, err
					}
					// Verify the update took place inside the transaction before the closing proof.
					lease, err := txQueries.GetFleetRuntimeLeaseForRecovery(ctx)
					require.NoError(t, err)
					require.Equal(t, replacement.DCSClusterID, lease.DcsClusterID)
					observed := replacement
					observed.DCSProofDeadline = time.Now().Add(tt.proofTime)
					return observed, tt.closingErr
				})
				return observer, func() {}, nil
			})

			wantClusterID := replacement.DCSClusterID
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.False(t, report.Rebound)
				wantClusterID = source.DCSClusterID
			} else {
				require.NoError(t, err)
				require.True(t, report.Rebound)
			}
			lease, err := queries.GetFleetRuntimeLeaseForRecovery(ctx)
			require.NoError(t, err)
			require.Equal(t, wantClusterID, lease.DcsClusterID)
			require.Equal(t, old.Token.LeaseEpoch, lease.LeaseEpoch)
		})
	}
}

func TestArmCommitDeadline(t *testing.T) {
	const margin = 20 * time.Millisecond

	for name, proofRemaining := range map[string]time.Duration{
		"expired":       -time.Second,
		"within margin": margin / 2,
	} {
		t.Run(name, func(t *testing.T) {
			timer, err := armCommitDeadline(proofRemaining, margin, func() { t.Error("cancel must not be armed") })
			require.ErrorContains(t, err, "leader proof expired")
			require.Nil(t, timer)
		})
	}

	t.Run("cancels one margin before the deadline", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const wait = 30 * time.Millisecond
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			timer, err := armCommitDeadline(wait+margin, margin, cancel)
			require.NoError(t, err)
			defer timer.Stop()

			time.Sleep(wait - time.Nanosecond)
			synctest.Wait()
			require.NoError(t, ctx.Err())

			time.Sleep(time.Nanosecond)
			synctest.Wait()
			require.ErrorIs(t, ctx.Err(), context.Canceled)
		})
	})
}
