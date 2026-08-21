package ha

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRuntimeStatusReportsInitializingThenHealthyPassive(t *testing.T) {
	// Arrange
	now := time.Now()
	coordinator := newCoordinatorWithHolder(staticObserver{}, &fakeLeaseStore{}, coordinatorTestConfig(), uuid.New())
	endpointOwned := false
	runtime := newRuntime(coordinator, newRuntimeTestGroup(), alwaysHealthy, RuntimeConfig{
		EndpointOwned: func() bool { return endpointOwned },
	})

	// Act and assert
	require.Equal(t, Status{
		Role: RoleInitializing, Observation: ObservationUnavailable,
		Endpoint: EndpointNotApplicable, ReasonCodes: []ReasonCode{ReasonObservationPending},
	}, runtime.Status(now))

	coordinator.deactivate(nil)
	status := runtime.Status(time.Now())
	require.Equal(t, RolePassive, status.Role)
	require.Equal(t, ObservationCurrent, status.Observation)
	require.Empty(t, status.ReasonCodes)
	require.True(t, runtime.Passive(time.Now()))

	endpointOwned = true
	status = runtime.Status(time.Now())
	require.Equal(t, RoleDegraded, status.Role)
	require.Equal(t, EndpointUnhealthy, status.Endpoint)
	require.Equal(t, []ReasonCode{ReasonEndpointUnhealthy}, status.ReasonCodes)
	require.False(t, runtime.Passive(time.Now()))
}

func TestRuntimeStatusDegradesUnavailableAndStaleObservations(t *testing.T) {
	// Arrange
	coordinator := newCoordinatorWithHolder(staticObserver{}, &fakeLeaseStore{}, coordinatorTestConfig(), uuid.New())
	runtime := newRuntime(coordinator, newRuntimeTestGroup(), alwaysHealthy, RuntimeConfig{})

	// Act and assert
	coordinator.deactivate(errors.New("secret topology details"))
	unavailable := runtime.Status(time.Now())
	require.Equal(t, RoleDegraded, unavailable.Role)
	require.Equal(t, ObservationUnavailable, unavailable.Observation)
	require.Equal(t, []ReasonCode{ReasonControlPlaneUnavailable}, unavailable.ReasonCodes)

	coordinator.deactivate(nil)
	stale := runtime.Status(time.Now().Add(2 * time.Second))
	require.Equal(t, RoleDegraded, stale.Role)
	require.Equal(t, ObservationStale, stale.Observation)
	require.Equal(t, []ReasonCode{ReasonObservationStale}, stale.ReasonCodes)
}

func TestRuntimeStatusReportsActiveOnlyAfterAdmissionAndEndpointHealth(t *testing.T) {
	// Arrange
	coordinator := newCoordinatorWithHolder(staticObserver{}, &fakeLeaseStore{}, coordinatorTestConfig(), uuid.New())
	coordinator.mu.Lock()
	coordinator.activeCtx, coordinator.cancelActive = context.WithCancelCause(t.Context())
	coordinator.ownership = Ownership{ExpiresAt: time.Now().Add(time.Second)}
	coordinator.observed = true
	coordinator.updatedAt = time.Now()
	coordinator.mu.Unlock()
	endpointHealthy := true
	runtime := newRuntime(coordinator, newRuntimeTestGroup(), alwaysHealthy, RuntimeConfig{
		EndpointHealthy: func() bool { return endpointHealthy },
	})

	// Act and assert
	pending := runtime.Status(time.Now())
	require.Equal(t, RoleInitializing, pending.Role)
	require.Equal(t, []ReasonCode{ReasonActivationPending}, pending.ReasonCodes)

	runtime.gate.activate(t.Context())
	active := runtime.Status(time.Now())
	require.Equal(t, RoleActive, active.Role)
	require.Equal(t, EndpointHealthy, active.Endpoint)
	require.NotNil(t, active.LeaseExpiresAt)

	endpointHealthy = false
	degraded := runtime.Status(time.Now())
	require.Equal(t, RoleDegraded, degraded.Role)
	require.Equal(t, EndpointUnhealthy, degraded.Endpoint)
	require.Nil(t, degraded.LeaseExpiresAt)
}
