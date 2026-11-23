package deployment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const (
	leaseRecoveryTimeout = 10 * time.Second
	leaseCommitMargin    = time.Second
)

// FleetLeaseRecoveryRequest identifies the approved source and replacement etcd clusters.
type FleetLeaseRecoveryRequest struct {
	Apply         bool
	OldClusterID  uint64
	NewClusterID  uint64
	WritersFenced bool
}

func (r FleetLeaseRecoveryRequest) validate() error {
	if r == (FleetLeaseRecoveryRequest{}) {
		return nil
	}
	if !r.Apply || !r.WritersFenced || r.OldClusterID == 0 || r.NewClusterID == 0 || r.OldClusterID == r.NewClusterID {
		return errors.New("lease recovery requires --apply, --writers-fenced and two distinct nonzero cluster IDs")
	}
	return nil
}

type FleetLeaseRecoveryReport struct {
	CurrentClusterID        string `json:"current_etcd_cluster_id"`
	StoredClusterID         string `json:"stored_etcd_cluster_id,omitempty"`
	Writer                  string `json:"database_writer"`
	CurrentWriterGeneration int64  `json:"current_writer_generation"`
	StoredWriterGeneration  int64  `json:"stored_writer_generation,omitempty"`
	LeaseEpoch              int64  `json:"lease_epoch,omitempty"`
	LeaseExpired            bool   `json:"lease_expired"`
	Rebound                 bool   `json:"rebound"`
}

type leaseRecoveryObserver interface {
	ObserveAndRun(
		ctx context.Context,
		action func(context.Context, ha.WriterObservation) error,
	) (ha.WriterObservation, error)
}

// RecoverFleetLease inspects or rebinds the expired lease in a restored database.
func RecoverFleetLease(ctx context.Context, envPath string, request FleetLeaseRecoveryRequest) (FleetLeaseRecoveryReport, error) {
	if err := request.validate(); err != nil {
		return FleetLeaseRecoveryReport{}, err
	}
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return FleetLeaseRecoveryReport{}, err
	}
	if err := validateNodeConfig(config); err != nil {
		return FleetLeaseRecoveryReport{}, err
	}
	if !config.isDatabaseNode() {
		return FleetLeaseRecoveryReport{}, errors.New("lease recovery must run on ha-a or ha-b")
	}
	etcdConfig, err := fleetObserverEtcdConfig(config)
	if err != nil {
		return FleetLeaseRecoveryReport{}, err
	}
	conn, err := openWriterProbeDatabase(config)
	if err != nil {
		return FleetLeaseRecoveryReport{}, err
	}
	defer conn.Close()

	report, err := recoverFleetLeaseTransaction(ctx, conn, request, func(queries sqlc.Querier) (leaseRecoveryObserver, func(), error) {
		return newDeploymentWriterObserver(etcdConfig, queries)
	})
	if err != nil {
		return FleetLeaseRecoveryReport{}, fmt.Errorf("recover Fleet lease (inspect the lease before retry): %w", err)
	}
	return report, nil
}

func recoverFleetLeaseTransaction(
	ctx context.Context,
	conn *sql.DB,
	request FleetLeaseRecoveryRequest,
	newObserver func(sqlc.Querier) (leaseRecoveryObserver, func(), error),
) (FleetLeaseRecoveryReport, error) {
	ctx, cancel := context.WithTimeout(ctx, leaseRecoveryTimeout)
	defer cancel()
	var proofTimer *time.Timer
	defer func() {
		if proofTimer != nil {
			proofTimer.Stop()
		}
	}()

	return db.WithTransactionNoRetry(ctx, conn, func(queries sqlc.Querier) (FleetLeaseRecoveryReport, error) {
		observer, closeObserver, err := newObserver(queries)
		if err != nil {
			return FleetLeaseRecoveryReport{}, err
		}
		defer closeObserver()

		var report FleetLeaseRecoveryReport
		observed, err := observer.ObserveAndRun(ctx, func(ctx context.Context, writer ha.WriterObservation) error {
			var err error
			report, err = inspectOrRebindFleetLease(ctx, queries, writer, request)
			return err
		})
		if err != nil {
			return FleetLeaseRecoveryReport{}, err
		}
		if report.Rebound {
			// The transaction helper uses this context for the commit.
			proofTimer, err = armCommitDeadline(time.Until(observed.DCSProofDeadline), leaseCommitMargin, cancel)
			if err != nil {
				return FleetLeaseRecoveryReport{}, err
			}
		}
		return report, nil
	}, &sql.TxOptions{ReadOnly: !request.Apply})
}

// armCommitDeadline cancels the commit one margin before the etcd leader proof runs out.
func armCommitDeadline(proofRemaining, margin time.Duration, cancel context.CancelFunc) (*time.Timer, error) {
	remaining := proofRemaining - margin
	if remaining <= 0 {
		return nil, errors.New("etcd leader proof expired before lease recovery could commit")
	}
	return time.AfterFunc(remaining, cancel), nil
}

func inspectOrRebindFleetLease(ctx context.Context, queries sqlc.Querier, writer ha.WriterObservation, request FleetLeaseRecoveryRequest) (FleetLeaseRecoveryReport, error) {
	report := FleetLeaseRecoveryReport{
		CurrentClusterID: writer.DCSClusterID, Writer: writer.LeaderName, CurrentWriterGeneration: writer.WriterGeneration,
	}
	if request.Apply && writer.DCSClusterID != strconv.FormatUint(request.NewClusterID, 10) {
		return report, errors.New("current etcd cluster does not match --new-cluster-id")
	}

	lease, err := queries.GetFleetRuntimeLeaseForRecovery(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return report, nil
	}
	if err != nil {
		return report, fmt.Errorf("read Fleet runtime lease: %w", err)
	}
	report.StoredClusterID, report.StoredWriterGeneration, report.LeaseEpoch, report.LeaseExpired =
		lease.DcsClusterID, lease.HighestWriterGeneration, lease.LeaseEpoch, lease.Expired
	if !request.Apply {
		return report, nil
	}
	if lease.DcsClusterID == writer.DCSClusterID {
		if writer.WriterGeneration < lease.HighestWriterGeneration {
			return report, errors.New("current writer generation is older than the lease for the same etcd cluster")
		}
		return report, nil
	}
	if lease.DcsClusterID != strconv.FormatUint(request.OldClusterID, 10) {
		return report, errors.New("stored etcd cluster does not match --old-cluster-id")
	}
	if !lease.Expired {
		return report, errors.New("Fleet runtime lease has not expired")
	}

	epoch, err := queries.RebindExpiredFleetRuntimeLease(ctx, sqlc.RebindExpiredFleetRuntimeLeaseParams{
		NewClusterID: writer.DCSClusterID, WriterGeneration: writer.WriterGeneration, OldClusterID: lease.DcsClusterID,
		ServerAddress: writer.ServerAddress, ServerPort: writer.ServerPort, Timeline: writer.Timeline,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return report, errors.New("Fleet runtime lease or PostgreSQL writer changed during recovery")
	}
	if err != nil {
		return report, fmt.Errorf("rebind Fleet runtime lease: %w", err)
	}
	report.StoredClusterID, report.StoredWriterGeneration, report.LeaseEpoch, report.Rebound =
		writer.DCSClusterID, writer.WriterGeneration, epoch, true
	return report, nil
}
