package pairing

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
)

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
	filter FleetNodeDiscoveredDeviceFilter
}

func (s *pagingPairTargetStore) ListFleetNodeDiscoveredDevices(_ context.Context, _ int64, _ *int64, filter FleetNodeDiscoveredDeviceFilter) ([]FleetNodeDiscoveredDevice, error) {
	s.calls = append(s.calls, pagingPairTargetCall{filter: copyFleetNodeDiscoveredDeviceFilter(filter)})
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
