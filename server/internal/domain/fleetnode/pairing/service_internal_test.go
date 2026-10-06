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
	identifier    string
	identifierErr error
	devices       []FleetNodeDevice
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

func (s *pairServiceStore) LockDeviceForFleetNodePairing(context.Context, int64, int64) (bool, error) {
	return true, nil
}

func (s *pairServiceStore) PairDeviceToFleetNode(context.Context, int64, int64, int64, *int64) (int64, error) {
	return 1, nil
}

func (s *pairServiceStore) TransferDiscoveredDeviceAttribution(context.Context, int64, int64, int64) (int64, error) {
	return 0, nil
}

func (s *pairServiceStore) GetFleetNodePairedDeviceIdentifier(context.Context, int64, int64) (string, error) {
	return s.identifier, s.identifierErr
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
		orgID       int64
		userID      int64
		identifiers []string
	}, 1)
	svc := NewService(
		&pairServiceStore{identifier: "node-device"},
		pairServiceEnrollmentStore{},
		passThroughTransactor{},
	).WithRigConfigReapplier(func(_ context.Context, orgID, userID int64, identifiers []string) {
		reapplied <- struct {
			orgID       int64
			userID      int64
			identifiers []string
		}{orgID: orgID, userID: userID, identifiers: identifiers}
	})

	require.NoError(t, svc.PairDevice(t.Context(), 12, 34, 56, &assignedBy))

	select {
	case got := <-reapplied:
		require.Equal(t, int64(56), got.orgID)
		require.Equal(t, assignedBy, got.userID)
		require.Equal(t, []string{"node-device"}, got.identifiers)
	case <-time.After(time.Second):
		t.Fatal("rig config reapply was not started")
	}
}

func TestPairDeviceDoesNotReapplyRigConfigWhenIdentifierUnavailable(t *testing.T) {
	for _, identifierErr := range []error{nil, errors.New("identifier lookup failed")} {
		t.Run(fmt.Sprint(identifierErr), func(t *testing.T) {
			assignedBy := int64(91)
			svc := NewService(
				&pairServiceStore{identifierErr: identifierErr},
				pairServiceEnrollmentStore{},
				passThroughTransactor{},
			).WithRigConfigReapplier(func(context.Context, int64, int64, []string) {
				t.Error("a failed lookup must not reapply rig config to the organization")
			})

			require.NoError(t, svc.PairDevice(t.Context(), 12, 34, 56, &assignedBy))
		})
	}
}

func TestFleetNodeRigConfigReapplyDetachesFromRequestCancellation(t *testing.T) {
	assignedBy := int64(91)
	called := false
	svc := &Service{rigConfigReapplier: func(ctx context.Context, orgID, userID int64, identifiers []string) {
		called = true
		require.NoError(t, ctx.Err())
		require.Equal(t, int64(56), orgID)
		require.Equal(t, assignedBy, userID)
		require.Equal(t, []string{"node-device"}, identifiers)
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	svc.reapplyRigConfigBestEffort(ctx, 56, &assignedBy, []string{"node-device"})
	require.True(t, called)

	called = false
	svc.reapplyRigConfigBestEffort(ctx, 56, &assignedBy, nil)
	require.False(t, called, "empty targets must not become an organization-wide request")
}
