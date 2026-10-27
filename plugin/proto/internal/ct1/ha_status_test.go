package ct1

import (
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestHAObservationsDegrade(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*mdk.HAStatus)
	}{
		{"missing peer inventory", func(h *mdk.HAStatus) { h.Members = h.Members[:1] }},
		{"duplicate member", func(h *mdk.HAStatus) { h.Members = append(h.Members, h.Members[0]) }},
		{"changed partner", func(h *mdk.HAStatus) { h.PeerServerId = "replacement" }},
		{"future role", func(h *mdk.HAStatus) {
			h.Members[0].RoleObservation.ObservedAt = timestamppb.New(time.Now().Add(time.Minute))
		}},
		{"stale role flag", func(h *mdk.HAStatus) {
			h.Members[0].RoleObservation.Freshness = mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_STALE
		}},
		{"missing role observation", func(h *mdk.HAStatus) { h.Members[0].RoleObservation = nil }},
		{"wrong observation source", func(h *mdk.HAStatus) { h.Members[0].RoleObservation.SourceServerId = "other" }},
		{"peer unreachable", func(h *mdk.HAStatus) { h.Members[1].FrigateReachable = new(false) }},
		{"peer reachability unknown", func(h *mdk.HAStatus) { h.Members[1].FrigateReachable = nil }},
		{"peer role stale", func(h *mdk.HAStatus) {
			h.Members[1].RoleObservation.ObservedAt = timestamppb.New(time.Now().Add(-time.Minute))
		}},
		{"cached role differs", func(h *mdk.HAStatus) {
			h.Members[0].HaRole = mdk.HARole_HA_ROLE_STANDBY
			h.Members[0].ContainerWritePrimary = new(false)
			h.Members[1].HaRole = mdk.HARole_HA_ROLE_PRIMARY
			h.Members[1].ContainerWritePrimary = new(true)
		}},
		{"cached authority unknown", func(h *mdk.HAStatus) { h.Members[0].ContainerWritePrimary = nil }},
		{"cached recovery missing", func(h *mdk.HAStatus) { h.Members[0].Recovery = nil }},
		{"retained identity during store outage", func(h *mdk.HAStatus) { h.IdentityState = mdk.HAIdentityState_HA_IDENTITY_STATE_UNAVAILABLE }},
		{"witness observation stale", func(h *mdk.HAStatus) {
			h.Members = append(h.Members, &mdk.HAMemberStatus{Id: "witness", Kind: mdk.HAMemberRole_HA_MEMBER_ROLE_WITNESS,
				EtcdReachable: new(true), EtcdObservation: &mdk.HAObservation{SourceServerId: h.ReportingServerId,
					ObservedAt: timestamppb.New(time.Now().Add(-2 * time.Minute)), Freshness: mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_FRESH}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
			a.primary.Store(true)
			d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
			require.NoError(t, err)
			defer d.Close(t.Context())
			a.haOverride = tc.change
			metrics, err := d.Status(t.Context())
			require.NoError(t, err)
			require.True(t, metrics.Container.HA.Degraded)

		})
	}
}

func TestHAWitnessNeedsOnlyFreshEtcdObservation(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	a.haOverride = func(h *mdk.HAStatus) {
		h.ObservedAt = timestamppb.New(time.Now().Add(-time.Hour))
		h.Members[0].InventoryObservation = &mdk.HAObservation{ObservedAt: h.ObservedAt}
		h.Members = append(h.Members, &mdk.HAMemberStatus{Id: "witness", Kind: mdk.HAMemberRole_HA_MEMBER_ROLE_WITNESS,
			EtcdReachable: new(true), EtcdObservation: &mdk.HAObservation{SourceServerId: h.ReportingServerId,
				ObservedAt: timestamppb.New(time.Now().Add(-time.Minute)), Freshness: mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_FRESH}})
	}
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	metrics, err := d.Status(t.Context())
	require.NoError(t, err)
	require.False(t, metrics.Container.HA.Degraded, "witnesses have no Frigate process; etcd has a separate 90-second lifetime")
}

func TestHASavedClusterIdentityCannotChange(t *testing.T) {
	for _, change := range []struct {
		name   string
		report func(*mdk.HAStatus)
	}{
		{"different cluster", func(h *mdk.HAStatus) { h.ClusterId = "ae4cbef0-74ae-44cc-9354-3e8bba79d688" }},
		{"conflicting identity", func(h *mdk.HAStatus) { h.IdentityState = mdk.HAIdentityState_HA_IDENTITY_STATE_CONFLICTED }},
	} {
		t.Run(change.name, func(t *testing.T) {
			a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
			a.primary.Store(true)
			d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
			require.NoError(t, err)
			defer d.Close(t.Context())
			a.server.Stop()
			b.primary.Store(true)
			b.haOverride = change.report
			_, _, err = d.DescribeDevice(t.Context())
			require.Error(t, err, "pairing must verify the saved cluster before accepting it")
			_, err = d.Status(t.Context())
			require.Error(t, err)
			require.Error(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
			require.Zero(t, b.mapReads.Load())
			require.Zero(t, b.writes.Load())
		})
	}
}

func TestHASavedClusterWorksWhileInventoryLoads(t *testing.T) {
	for _, identity := range []mdk.HAIdentityState{
		mdk.HAIdentityState_HA_IDENTITY_STATE_CONFIRMED,
		mdk.HAIdentityState_HA_IDENTITY_STATE_UNAVAILABLE,
	} {
		t.Run(identity.String(), func(t *testing.T) {
			a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
			a.server.Stop()
			b.primary.Store(true)
			b.haOverride = func(h *mdk.HAStatus) {
				h.InventoryReady = false
				h.IdentityState = identity
			}
			d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
			require.NoError(t, err, "a Fleet restart must retain access to the verified surviving controller")
			defer d.Close(t.Context())
			metrics, err := d.Status(t.Context())
			require.NoError(t, err)
			require.True(t, metrics.Container.HA.Degraded)
			require.EqualValues(t, 1, b.mapReads.Load())
			require.NoError(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
			require.EqualValues(t, 1, b.writes.Load())
		})
	}
}

func TestHACachedPrimaryNeverGrantsWriteAuthority(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	a.haOverride = func(h *mdk.HAStatus) {
		h.Members[0].RoleObservation.ObservedAt = timestamppb.New(time.Now().Add(-time.Minute))
	}
	a.statusOverride = func(s *mdk.SystemStatus) { s.ContainerWritePrimary = false }
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	req := sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}
	require.Error(t, d.SetContainerControl(t.Context(), req))
	require.Zero(t, a.writes.Load())
	require.Zero(t, b.writes.Load())
	// Fresh live authority can permit writes while cached health remains stale.
	a.statusOverride = nil
	require.NoError(t, d.SetContainerControl(t.Context(), req))
	require.EqualValues(t, 1, a.writes.Load())
	metrics, err := d.Status(t.Context())
	require.NoError(t, err)
	require.True(t, metrics.Container.HA.Degraded)
	a.haOverride = nil
	metrics, err = d.Status(t.Context())
	require.NoError(t, err)
	require.False(t, metrics.Container.HA.Degraded)
}
