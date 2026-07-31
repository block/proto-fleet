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
	devices    []FleetNodeDevice
}

func (s *pairServiceStore) ListFleetNodeDevices(context.Context, int64, *int64) ([]FleetNodeDevice, error) {
	return append([]FleetNodeDevice(nil), s.devices...), nil
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

func (s *pairServiceStore) DeleteMinerCredentialsByDeviceIDAndOrgID(context.Context, int64, int64) (int64, error) {
	return 0, nil
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

func TestPairDeviceReappliesRigConfigAfterCommit(t *testing.T) {
	assignedBy := int64(91)
	reapplied := make(chan struct {
		orgID  int64
		userID int64
	}, 1)
	svc := NewService(
		&pairServiceStore{identifier: "node-device"},
		pairServiceEnrollmentStore{},
		passThroughTransactor{},
	).WithRigConfigReapplier(func(_ context.Context, orgID, userID int64) {
		reapplied <- struct {
			orgID  int64
			userID int64
		}{orgID: orgID, userID: userID}
	})

	require.NoError(t, svc.PairDevice(t.Context(), 12, 34, 56, &assignedBy))

	select {
	case got := <-reapplied:
		require.Equal(t, int64(56), got.orgID)
		require.Equal(t, assignedBy, got.userID)
	case <-time.After(time.Second):
		t.Fatal("rig config reapply was not started")
	}
}

func TestFleetNodeConnectedReappliesRigConfigWithAssignmentIdentity(t *testing.T) {
	assignedBy := int64(92)
	reapplied := make(chan struct {
		orgID  int64
		userID int64
	}, 1)
	store := &pairServiceStore{devices: []FleetNodeDevice{{
		FleetNodeID: 12,
		DeviceID:    34,
		AssignedBy:  &assignedBy,
	}}}
	svc := NewService(store, pairServiceEnrollmentStore{}, passThroughTransactor{}).
		WithRigConfigReapplier(func(_ context.Context, orgID, userID int64) {
			reapplied <- struct {
				orgID  int64
				userID int64
			}{orgID: orgID, userID: userID}
		})

	svc.FleetNodeConnectedBestEffort(t.Context(), 12, 56)

	select {
	case got := <-reapplied:
		require.Equal(t, int64(56), got.orgID)
		require.Equal(t, assignedBy, got.userID)
	case <-time.After(time.Second):
		t.Fatal("rig config reapply was not started")
	}
}

func TestRigConfigReapplyCoalescesConcurrentOrgTriggers(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	svc := &Service{rigConfigReapplier: func(context.Context, int64, int64) {
		started <- struct{}{}
		<-release
	}}
	done := make(chan struct{}, 2)
	reapply := func() {
		svc.runRigConfigReapply(t.Context(), 56, 91)
		done <- struct{}{}
	}

	go reapply()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first rig config reapply was not started")
	}
	go reapply()

	select {
	case <-started:
		t.Fatal("concurrent organization-wide reapply was not coalesced")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	for range 2 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("coalesced rig config reapply did not finish")
		}
	}
	select {
	case <-started:
		t.Fatal("coalesced trigger started a second reapply")
	default:
	}
}
