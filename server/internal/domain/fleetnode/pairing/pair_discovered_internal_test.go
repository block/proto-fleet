package pairing

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	storemocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type orderedPersistStore struct {
	Store
	events        *[]string
	activePairing bool
}

func (s *orderedPersistStore) GetDeviceIDByDeviceIdentifier(context.Context, string) (int64, error) {
	*s.events = append(*s.events, "resolve_device")
	return 42, nil
}

func (s *orderedPersistStore) LockDeviceForFleetNodePairing(context.Context, int64, int64) (bool, error) {
	*s.events = append(*s.events, "lock_device")
	return true, nil
}

func (s *orderedPersistStore) DeviceHasActiveCloudPairing(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (s *orderedPersistStore) DeviceHasActivePairing(context.Context, int64, int64) (bool, error) {
	*s.events = append(*s.events, "check_active_pairing")
	return s.activePairing, nil
}

func (s *orderedPersistStore) PairDeviceToFleetNode(context.Context, int64, int64, int64, *int64) (int64, error) {
	return 1, nil
}

func (s *orderedPersistStore) DeleteMinerCredentialsByDeviceIDAndOrgID(context.Context, int64, int64) (int64, error) {
	return 0, nil
}

func (s *orderedPersistStore) TransferDiscoveredDeviceAttribution(context.Context, int64, int64, int64) (int64, error) {
	return 1, nil
}

type orderedPersistEnrollmentStore struct {
	enrollment.AgentStore
	events *[]string
}

func (s orderedPersistEnrollmentStore) LockFleetNodeByID(context.Context, int64, int64) (*enrollment.FleetNode, error) {
	*s.events = append(*s.events, "lock_node")
	return &enrollment.FleetNode{EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}, nil
}

type errorEnrollmentStore struct {
	enrollment.AgentStore
	err error
}

func (s errorEnrollmentStore) LockFleetNodeByID(context.Context, int64, int64) (*enrollment.FleetNode, error) {
	return nil, s.err
}

func TestPersistPairResultLocksNodeThenDeviceBeforeWrites(t *testing.T) {
	for _, tc := range []struct {
		name           string
		outcome        gatewaypb.PairOutcome
		existingStatus string
	}{
		{name: "paired", outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED},
		{name: "auth_needed", outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED},
		{name: "auth_failed", outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_FAILED},
		{name: "preserve_paired", outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED, existingStatus: StatusPaired},
		{name: "preserve_default_password", outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_FAILED, existingStatus: StatusDefaultPassword},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			deviceStore := storemocks.NewMockDeviceStore(ctrl)
			discoveredStore := storemocks.NewMockDiscoveredDeviceStore(ctrl)
			events := []string{}
			fleetNodeID := int64(12)
			orgID := int64(34)
			identifier := "mac:ordered"
			dd := &discoverymodels.DiscoveredDevice{
				Device:                  pairingpb.Device{DeviceIdentifier: identifier},
				OrgID:                   orgID,
				DiscoveredByFleetNodeID: &fleetNodeID,
			}
			existing := &pairingpb.Device{DeviceIdentifier: identifier}
			discoveredStore.EXPECT().GetDevice(gomock.Any(), gomock.Any()).Return(dd, nil)
			deviceStore.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), identifier, orgID).Return(existing, nil)
			wantEvents := []string{"lock_node", "resolve_device", "lock_device"}
			wantStatus := StatusPaired
			if tc.outcome != gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED {
				wantEvents = append(wantEvents, "check_active_pairing")
				wantStatus = StatusAuthenticationNeeded
			}
			if tc.existingStatus != "" {
				wantStatus = tc.existingStatus
				deviceStore.EXPECT().GetDevicePairingStatusByIdentifier(gomock.Any(), identifier, orgID).Return(tc.existingStatus, nil)
			} else {
				wantEvents = append(wantEvents, "save_discovered", "update_device")
				discoveredStore.EXPECT().Save(gomock.Any(), gomock.Any(), dd).DoAndReturn(
					func(context.Context, discoverymodels.DeviceOrgIdentifier, *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error) {
						events = append(events, "save_discovered")
						return dd, nil
					},
				)
				deviceStore.EXPECT().UpdateDeviceInfo(gomock.Any(), gomock.Any(), orgID).DoAndReturn(
					func(context.Context, *pairingpb.Device, int64) error {
						events = append(events, "update_device")
						return nil
					},
				)
				if tc.outcome == gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED {
					deviceStore.EXPECT().UpsertDevicePairing(gomock.Any(), gomock.Any(), orgID, StatusPaired).Return(nil)
					deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), gomock.Any(), gomock.Any(), "").Return(nil)
				} else {
					deviceStore.EXPECT().SetDevicePairingAuthNeededIfNotPaired(gomock.Any(), gomock.Any(), orgID).Return(true, nil)
				}
			}

			service := NewService(
				&orderedPersistStore{events: &events, activePairing: tc.existingStatus != ""},
				orderedPersistEnrollmentStore{events: &events},
				passThroughTransactor{},
			).WithProvisioning(deviceStore, discoveredStore, nil)
			defaultPasswordActive := false
			status, err := service.PersistFleetNodePairResult(t.Context(), fleetNodeID, orgID, &gatewaypb.FleetNodePairResult{
				DeviceIdentifier:      identifier,
				Outcome:               tc.outcome,
				DefaultPasswordActive: &defaultPasswordActive,
			}, nil)

			require.NoError(t, err)
			require.Equal(t, wantStatus, status)
			require.Equal(t, wantEvents, events)
		})
	}
}

func TestPairingLockPreservesRetryablePostgresError(t *testing.T) {
	retryable := &pgconn.PgError{Code: db.PGDeadlockDetected}
	service := NewService(nil, errorEnrollmentStore{err: retryable}, passThroughTransactor{})

	err := service.lockFleetNodeForPairing(t.Context(), 12, 34)

	require.Same(t, retryable, err)
}

func TestPairingStatusFilterSet(t *testing.T) {
	got, supported := pairingStatusFilterValues(nil)
	require.False(t, supported)
	assert.Nil(t, got)

	got, supported = pairingStatusFilterValues([]fleetmanagementv1.PairingStatus{
		fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
		fleetmanagementv1.PairingStatus_PAIRING_STATUS_UNPAIRED,
		fleetmanagementv1.PairingStatus_PAIRING_STATUS_FAILED,
	})

	require.True(t, supported)
	assert.Equal(t, []string{StatusAuthenticationNeeded, "", StatusUnpaired, StatusFailed}, got)

	got, supported = pairingStatusFilterValues([]fleetmanagementv1.PairingStatus{
		fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
		fleetmanagementv1.PairingStatus_PAIRING_STATUS_PAIRED,
	})
	require.True(t, supported)
	assert.Equal(t, []string{StatusAuthenticationNeeded}, got)

	got, supported = pairingStatusFilterValues([]fleetmanagementv1.PairingStatus{
		fleetmanagementv1.PairingStatus_PAIRING_STATUS_PAIRED,
	})
	assert.False(t, supported)
	assert.Nil(t, got)

	got, supported = pairingStatusFilterValues([]fleetmanagementv1.PairingStatus{
		fleetmanagementv1.PairingStatus(999),
	})
	assert.False(t, supported)
	assert.Nil(t, got)
}

type pagingPairTargetStore struct {
	Store
	devices []FleetNodeDiscoveredDevice
	calls   []pagingPairTargetCall
}

type pagingPairTargetCall struct {
	filter      FleetNodeDiscoveredDeviceFilter
	orgID       int64
	fleetNodeID *int64
}

func (s *pagingPairTargetStore) ListFleetNodeDiscoveredDevices(_ context.Context, orgID int64, fleetNodeID *int64, filter FleetNodeDiscoveredDeviceFilter) ([]FleetNodeDiscoveredDevice, error) {
	s.calls = append(s.calls, pagingPairTargetCall{filter: copyFleetNodeDiscoveredDeviceFilter(filter), orgID: orgID, fleetNodeID: copyInt64(fleetNodeID)})
	filtered := make([]FleetNodeDiscoveredDevice, 0, len(s.devices))
	for _, device := range s.devices {
		if filter.ExcludeAuthNeeded && device.PairingStatus == StatusAuthenticationNeeded {
			continue
		}
		if filter.Identifiers != nil && !slices.Contains(filter.Identifiers, device.DeviceIdentifier) {
			continue
		}
		if filter.PairingStatuses != nil && !slices.Contains(filter.PairingStatuses, device.PairingStatus) {
			continue
		}
		if filter.Models != nil && !slices.Contains(filter.Models, device.Model) {
			continue
		}
		if filter.Manufacturers != nil && !slices.Contains(filter.Manufacturers, device.Manufacturer) {
			continue
		}
		filtered = append(filtered, device)
	}
	start := 0
	if filter.CursorID != nil {
		for start < len(filtered) && filtered[start].ID <= *filter.CursorID {
			start++
		}
	}
	end := len(filtered)
	if filter.Limit != nil && start+int(*filter.Limit) < end {
		end = start + int(*filter.Limit)
	}
	return filtered[start:end], nil
}

func TestResolvePairAllPagesPreserveScopeAndCredentials(t *testing.T) {
	password := ""
	for _, credentials := range []*pairingpb.Credentials{nil, {Password: &password}} {
		store := pairAllTestStore(MaxPairBatch + 2)
		store.devices[MaxPairBatch].PairingStatus = StatusAuthenticationNeeded
		service := NewService(store, nil, nil)
		_, cursor, err := service.resolvePairTargetsPage(t.Context(), 7, 20, nil, true, credentials, nil)
		require.NoError(t, err)
		require.NotNil(t, cursor)
		targets, next, err := service.resolvePairTargetsPage(t.Context(), 7, 20, nil, true, credentials, cursor)
		require.NoError(t, err)
		assert.Nil(t, next)
		usable := credentials != nil && credentials.Password != nil
		if usable {
			assert.Len(t, targets, 2)
		} else {
			assert.Len(t, targets, 1)
		}
		require.Len(t, store.calls, 2)
		for _, call := range store.calls {
			assert.Equal(t, int64(20), call.orgID)
			require.NotNil(t, call.fleetNodeID)
			assert.Equal(t, int64(7), *call.fleetNodeID)
			assert.Equal(t, !usable, call.filter.ExcludeAuthNeeded)
			assert.Equal(t, int64(MaxPairBatch), *call.filter.Limit)
		}
	}
}

func copyFleetNodeDiscoveredDeviceFilter(filter FleetNodeDiscoveredDeviceFilter) FleetNodeDiscoveredDeviceFilter {
	return FleetNodeDiscoveredDeviceFilter{
		Identifiers:       slices.Clone(filter.Identifiers),
		PairingStatuses:   slices.Clone(filter.PairingStatuses),
		Models:            slices.Clone(filter.Models),
		Manufacturers:     slices.Clone(filter.Manufacturers),
		CursorID:          copyInt64(filter.CursorID),
		Limit:             copyInt64(filter.Limit),
		ExcludeAuthNeeded: filter.ExcludeAuthNeeded,
	}
}

func copyInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func TestResolvePairTargetsByFilterPagePassesSupportedFiltersToStore(t *testing.T) {
	devices := make([]FleetNodeDiscoveredDevice, 0, MaxPairBatch+2)
	for i := 1; i <= MaxPairBatch+1; i++ {
		devices = append(devices, FleetNodeDiscoveredDevice{
			ID:               int64(i),
			DeviceIdentifier: "mac:skip",
			IPAddress:        "10.0.0.1",
			Port:             "80",
			URLScheme:        "http",
			Model:            "M30",
			PairingStatus:    StatusAuthenticationNeeded,
		})
	}
	devices[len(devices)-1].DeviceIdentifier = "mac:match"
	devices[len(devices)-1].Model = "S19"
	devices[len(devices)-1].Manufacturer = "Bitmain"
	store := &pagingPairTargetStore{devices: devices}
	service := NewService(store, nil, nil)
	pw := "pw"

	targets, _, err := service.ResolvePairTargetsByFilterPage(t.Context(), 10, 20, &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED},
		Models:        []string{"S19"},
		Manufacturers: []string{"Bitmain"},
	}, &pairingpb.Credentials{Username: "root", Password: &pw}, nil)

	require.NoError(t, err)
	require.Equal(t, []string{"mac:match"}, internalTargetIdentifiers(targets))
	require.Len(t, store.calls, 1)
	assert.Equal(t, []string{StatusAuthenticationNeeded}, store.calls[0].filter.PairingStatuses)
	assert.Equal(t, []string{"S19"}, store.calls[0].filter.Models)
	assert.Equal(t, []string{"Bitmain"}, store.calls[0].filter.Manufacturers)
	assert.Nil(t, store.calls[0].filter.CursorID)
	assert.Equal(t, int64(MaxPairBatch), *store.calls[0].filter.Limit)
	assert.False(t, store.calls[0].filter.ExcludeAuthNeeded)
}

func TestResolvePairTargetsByFilterPageNormalizesEmptyModelManufacturerFilters(t *testing.T) {
	store := &pagingPairTargetStore{devices: []FleetNodeDiscoveredDevice{{
		ID:               1,
		DeviceIdentifier: "mac:authneeded",
		IPAddress:        "10.0.0.1",
		Port:             "80",
		URLScheme:        "http",
		Model:            "S19",
		Manufacturer:     "Bitmain",
		PairingStatus:    StatusAuthenticationNeeded,
	}}}
	service := NewService(store, nil, nil)
	pw := "pw"

	targets, _, err := service.ResolvePairTargetsByFilterPage(t.Context(), 10, 20, &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED},
		Models:        []string{},
		Manufacturers: []string{},
	}, &pairingpb.Credentials{Username: "root", Password: &pw}, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"mac:authneeded"}, internalTargetIdentifiers(targets))
	require.Len(t, store.calls, 1)
	assert.Nil(t, store.calls[0].filter.Models)
	assert.Nil(t, store.calls[0].filter.Manufacturers)
}

func TestResolvePairTargetsByFilterPageReturnsCursorAtBatchLimit(t *testing.T) {
	devices := make([]FleetNodeDiscoveredDevice, 0, MaxPairBatch+1)
	for i := 1; i <= MaxPairBatch+1; i++ {
		devices = append(devices, FleetNodeDiscoveredDevice{
			ID:               int64(i),
			DeviceIdentifier: "mac:page",
			IPAddress:        "10.0.0.1",
			Port:             "80",
			URLScheme:        "http",
			Model:            "S19",
		})
	}
	devices[MaxPairBatch].DeviceIdentifier = "mac:last"
	store := &pagingPairTargetStore{devices: devices}
	service := NewService(store, nil, nil)
	pw := "pw"

	targets, nextCursor, err := service.ResolvePairTargetsByFilterPage(t.Context(), 10, 20, &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_UNPAIRED},
		Models:        []string{"S19"},
	}, &pairingpb.Credentials{Username: "root", Password: &pw}, nil)

	require.NoError(t, err)
	require.Len(t, targets, MaxPairBatch)
	require.NotNil(t, nextCursor)
	assert.Equal(t, int64(MaxPairBatch), *nextCursor)

	targets, nextCursor, err = service.ResolvePairTargetsByFilterPage(t.Context(), 10, 20, &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_UNPAIRED},
		Models:        []string{"S19"},
	}, &pairingpb.Credentials{Username: "root", Password: &pw}, nextCursor)

	require.NoError(t, err)
	assert.Equal(t, []string{"mac:last"}, internalTargetIdentifiers(targets))
	assert.Nil(t, nextCursor)
	require.Len(t, store.calls, 2)
	require.NotNil(t, store.calls[1].filter.CursorID)
	assert.Equal(t, int64(MaxPairBatch), *store.calls[1].filter.CursorID)
}

func TestResolvePairTargetsByFilterRejectsUnsupportedDeviceStatus(t *testing.T) {
	store := &pagingPairTargetStore{devices: []FleetNodeDiscoveredDevice{{
		ID:               1,
		DeviceIdentifier: "mac:authneeded",
		IPAddress:        "10.0.0.1",
		Port:             "80",
		URLScheme:        "http",
		Model:            "S19",
		PairingStatus:    StatusAuthenticationNeeded,
	}}}
	service := NewService(store, nil, nil)
	pw := "pw"

	targets, _, err := service.ResolvePairTargetsByFilterPage(t.Context(), 10, 20, &minercommandv1.DeviceFilter{
		DeviceStatus:  []fleetmanagementv1.DeviceStatus{fleetmanagementv1.DeviceStatus_DEVICE_STATUS_ONLINE},
		PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED},
		Models:        []string{"S19"},
	}, &pairingpb.Credentials{Username: "root", Password: &pw}, nil)

	require.NoError(t, err)
	assert.Empty(t, targets)
	assert.Empty(t, store.calls, "unsupported filters should stay on the cloud path without listing fleet-node targets")
}

func TestResolvePairTargetsByFilterRejectsUnsupportedPairingStatus(t *testing.T) {
	store := &pagingPairTargetStore{devices: []FleetNodeDiscoveredDevice{{
		ID:               1,
		DeviceIdentifier: "mac:paired",
		IPAddress:        "10.0.0.1",
		Port:             "80",
		URLScheme:        "http",
		Model:            "S19",
		PairingStatus:    StatusPaired,
	}}}
	service := NewService(store, nil, nil)
	pw := "pw"

	targets, _, err := service.ResolvePairTargetsByFilterPage(t.Context(), 10, 20, &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_PAIRED},
		Models:        []string{"S19"},
	}, &pairingpb.Credentials{Username: "root", Password: &pw}, nil)

	require.NoError(t, err)
	assert.Empty(t, targets)
	assert.Empty(t, store.calls, "unsupported pairing statuses should not widen the fleet-node filter")
}

func TestResolvePairTargetsByFilterEmptyAllDevicesStaysCloudOnly(t *testing.T) {
	store := &pagingPairTargetStore{devices: []FleetNodeDiscoveredDevice{
		{
			ID:               1,
			DeviceIdentifier: "mac:authneeded",
			IPAddress:        "10.0.0.1",
			Port:             "80",
			URLScheme:        "http",
			Model:            "S19",
			PairingStatus:    StatusAuthenticationNeeded,
		},
		{
			ID:               2,
			DeviceIdentifier: "mac:failed",
			IPAddress:        "10.0.0.2",
			Port:             "80",
			URLScheme:        "http",
			Model:            "S19",
			PairingStatus:    StatusFailed,
		},
	}}
	service := NewService(store, nil, nil)
	pw := "pw"

	targets, _, err := service.ResolvePairTargetsByFilterPage(t.Context(), 10, 20, &minercommandv1.DeviceFilter{}, &pairingpb.Credentials{Username: "root", Password: &pw}, nil)

	require.NoError(t, err)
	assert.Empty(t, targets)
	assert.Empty(t, store.calls)
}

func TestResolvePairTargetsByFilterUnpairedMatchesStoredUnpairedStatus(t *testing.T) {
	store := &pagingPairTargetStore{devices: []FleetNodeDiscoveredDevice{
		{
			ID:               1,
			DeviceIdentifier: "mac:no-row",
			IPAddress:        "10.0.0.1",
			Port:             "80",
			URLScheme:        "http",
			Model:            "S19",
			PairingStatus:    "",
		},
		{
			ID:               2,
			DeviceIdentifier: "mac:stored-unpaired",
			IPAddress:        "10.0.0.2",
			Port:             "80",
			URLScheme:        "http",
			Model:            "S19",
			PairingStatus:    StatusUnpaired,
		},
	}}
	service := NewService(store, nil, nil)
	pw := "pw"

	targets, _, err := service.ResolvePairTargetsByFilterPage(t.Context(), 10, 20, &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_UNPAIRED},
	}, &pairingpb.Credentials{Username: "root", Password: &pw}, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"mac:no-row", "mac:stored-unpaired"}, internalTargetIdentifiers(targets))
	require.Len(t, store.calls, 1)
	assert.Equal(t, []string{"", StatusUnpaired}, store.calls[0].filter.PairingStatuses)
}

func TestResolvePairTargetsByFilterMixedPairableAndImpossibleStatusesRoutesPairableSubset(t *testing.T) {
	store := &pagingPairTargetStore{devices: []FleetNodeDiscoveredDevice{
		{
			ID:               1,
			DeviceIdentifier: "mac:authneeded",
			IPAddress:        "10.0.0.1",
			Port:             "80",
			URLScheme:        "http",
			Model:            "S19",
			PairingStatus:    StatusAuthenticationNeeded,
		},
		{
			ID:               2,
			DeviceIdentifier: "mac:failed",
			IPAddress:        "10.0.0.2",
			Port:             "80",
			URLScheme:        "http",
			Model:            "S19",
			PairingStatus:    StatusFailed,
		},
	}}
	service := NewService(store, nil, nil)
	pw := "pw"

	targets, _, err := service.ResolvePairTargetsByFilterPage(t.Context(), 10, 20, &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{
			fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			fleetmanagementv1.PairingStatus_PAIRING_STATUS_PAIRED,
		},
	}, &pairingpb.Credentials{Username: "root", Password: &pw}, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"mac:authneeded"}, internalTargetIdentifiers(targets))
	require.Len(t, store.calls, 1)
	assert.Equal(t, []string{StatusAuthenticationNeeded}, store.calls[0].filter.PairingStatuses)
}

func TestResolvePairTargetsByFilterExcludesAuthNeededWithoutPassword(t *testing.T) {
	store := &pagingPairTargetStore{devices: []FleetNodeDiscoveredDevice{
		{
			ID:               1,
			DeviceIdentifier: "mac:authneeded",
			IPAddress:        "10.0.0.1",
			Port:             "80",
			URLScheme:        "http",
			Model:            "S19",
			PairingStatus:    StatusAuthenticationNeeded,
		},
		{
			ID:               2,
			DeviceIdentifier: "mac:unpaired",
			IPAddress:        "10.0.0.2",
			Port:             "80",
			URLScheme:        "http",
			Model:            "S19",
			PairingStatus:    "",
		},
	}}
	service := NewService(store, nil, nil)

	targets, _, err := service.ResolvePairTargetsByFilterPage(t.Context(), 10, 20, &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{
			fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			fleetmanagementv1.PairingStatus_PAIRING_STATUS_UNPAIRED,
		},
		Models: []string{"S19"},
	}, &pairingpb.Credentials{Username: "root"}, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"mac:unpaired"}, internalTargetIdentifiers(targets))
	require.Len(t, store.calls, 1)
	assert.True(t, store.calls[0].filter.ExcludeAuthNeeded)
}

func internalTargetIdentifiers(targets []*pairingpb.FleetNodePairTarget) []string {
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		out = append(out, target.GetDeviceIdentifier())
	}
	return out
}
