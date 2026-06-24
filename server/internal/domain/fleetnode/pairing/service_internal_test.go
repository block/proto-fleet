package pairing

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

type pairServiceStore struct {
	Store
	identifier string
}

func (s *pairServiceStore) DeviceExistsInOrg(context.Context, int64, int64) (bool, error) {
	return true, nil
}

func (s *pairServiceStore) DeviceHasActiveCloudPairing(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (s *pairServiceStore) PairDeviceToFleetNode(context.Context, int64, int64, int64, *int64) (int64, error) {
	return 1, nil
}

func (s *pairServiceStore) TransferDiscoveredDeviceAttribution(context.Context, int64, int64, int64) (int64, error) {
	return 0, nil
}

func (s *pairServiceStore) GetFleetNodePairedDeviceIdentifier(context.Context, int64, int64) (string, error) {
	return s.identifier, nil
}

type pairServiceEnrollmentStore struct {
	enrollment.AgentStore
}

func (s pairServiceEnrollmentStore) LockFleetNodeByID(context.Context, int64, int64) (*enrollment.FleetNode, error) {
	return &enrollment.FleetNode{EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}, nil
}

type passThroughTransactor struct{}

func (passThroughTransactor) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (passThroughTransactor) RunInTxWithResult(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	return fn(ctx)
}

var _ stores.Transactor = passThroughTransactor{}

type failingTelemetryScheduler struct{}

func (failingTelemetryScheduler) AddDevices(context.Context, ...telemetrymodels.DeviceIdentifier) error {
	return errors.New("scheduler down")
}

type blockingTelemetryScheduler struct {
	started chan struct{}
	release chan struct{}
}

func (s blockingTelemetryScheduler) AddDevices(ctx context.Context, _ ...telemetrymodels.DeviceIdentifier) error {
	close(s.started)
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for scheduler release: %w", ctx.Err())
	}
}

func TestPairDeviceIgnoresPostCommitTelemetrySchedulingFailure(t *testing.T) {
	oldTimeout := telemetryScheduleTimeout
	telemetryScheduleTimeout = time.Second
	t.Cleanup(func() { telemetryScheduleTimeout = oldTimeout })

	svc := NewService(
		&pairServiceStore{identifier: "node-device"},
		pairServiceEnrollmentStore{},
		passThroughTransactor{},
	).WithTelemetryScheduler(failingTelemetryScheduler{})

	err := svc.PairDevice(t.Context(), 12, 34, 56, nil)

	require.NoError(t, err)
}

func TestPairDeviceDoesNotBlockOnPostCommitTelemetryScheduling(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	svc := NewService(
		&pairServiceStore{identifier: "node-device"},
		pairServiceEnrollmentStore{},
		passThroughTransactor{},
	).WithTelemetryScheduler(blockingTelemetryScheduler{started: started, release: release})
	t.Cleanup(func() { close(release) })

	err := svc.PairDevice(t.Context(), 12, 34, 56, nil)

	require.NoError(t, err)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("telemetry scheduling was not started")
	}
}
