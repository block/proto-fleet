package collection

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubBuildingStore mirrors the fleetmanagement parse_filter test stub.
// It embeds the interface so unused methods fall through to a nil
// dispatch — validateFilterBuildings must only touch BuildingsByIDs.
type stubBuildingStore struct {
	interfaces.BuildingStore
	owned map[int64]struct{}
	err   error
}

func newOwnedStore(ids ...int64) *stubBuildingStore {
	s := &stubBuildingStore{owned: map[int64]struct{}{}}
	for _, id := range ids {
		s.owned[id] = struct{}{}
	}
	return s
}

func (s *stubBuildingStore) BuildingsByIDs(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := s.owned[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// callValidate is the test entry point. svc is constructed directly so
// these tests don't drag in the full collection service dependency
// graph — validateFilterBuildings only needs buildingStore.
func callValidate(t *testing.T, store interfaces.BuildingStore, filter *interfaces.DeviceSetFilter) error {
	t.Helper()
	svc := &Service{buildingStore: store}
	return svc.validateFilterBuildings(context.Background(), testOrgID, filter)
}

func TestValidateFilterBuildings_NilFilter(t *testing.T) {
	err := callValidate(t, nil, nil)
	require.NoError(t, err)
}

func TestValidateFilterBuildings_EmptyFilter(t *testing.T) {
	// No building IDs, no zone keys → nothing to validate, store is
	// untouched (pass nil to prove it).
	err := callValidate(t, nil, &interfaces.DeviceSetFilter{})
	require.NoError(t, err)
}

func TestValidateFilterBuildings_BuildingIDs_HappyPath(t *testing.T) {
	store := newOwnedStore(7, 9)
	err := callValidate(t, store, &interfaces.DeviceSetFilter{
		BuildingIDs: []int64{7, 9},
	})
	require.NoError(t, err)
}

func TestValidateFilterBuildings_BuildingIDs_RejectsZeroAndNegative(t *testing.T) {
	cases := []struct {
		name string
		ids  []int64
	}{
		{"zero", []int64{1, 0, 3}},
		{"negative", []int64{-5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := callValidate(t, nil, &interfaces.DeviceSetFilter{BuildingIDs: tc.ids})
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			assert.Contains(t, err.Error(), "building_ids")
		})
	}
}

func TestValidateFilterBuildings_BuildingIDs_RejectsCrossOrg(t *testing.T) {
	// Building 99 not in caller's org; 7 is. The bulk check rejects
	// when len(found) < len(requested). Error message must not echo
	// the rejected ID.
	store := newOwnedStore(7)
	err := callValidate(t, store, &interfaces.DeviceSetFilter{
		BuildingIDs: []int64{7, 99},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "building_ids")
	assert.NotContains(t, err.Error(), "99")
}

func TestValidateFilterBuildings_BuildingIDs_StoreError(t *testing.T) {
	store := &stubBuildingStore{err: errors.New("db down")}
	err := callValidate(t, store, &interfaces.DeviceSetFilter{BuildingIDs: []int64{7}})
	require.Error(t, err)
	// Internal errors (not InvalidArgument) when the bulk check itself fails.
	assert.False(t, fleeterror.IsInvalidArgumentError(err))
}

func TestValidateFilterBuildings_BuildingIDs_NilStoreWithExplicitIDs(t *testing.T) {
	// Defensive: nil store with explicit IDs is a server
	// misconfiguration, must surface as Internal so it shows up in
	// production logs.
	err := callValidate(t, nil, &interfaces.DeviceSetFilter{BuildingIDs: []int64{7}})
	require.Error(t, err)
	assert.False(t, fleeterror.IsInvalidArgumentError(err))
}

func TestValidateFilterBuildings_ZoneKeys_AllWildcard(t *testing.T) {
	// Wildcard entries (BuildingID == 0) don't trigger the cross-org
	// check; nil store proves the helper never calls into it.
	err := callValidate(t, nil, &interfaces.DeviceSetFilter{
		ZoneKeys: []interfaces.ZoneKey{
			{BuildingID: 0, Zone: "building-a"},
			{BuildingID: 0, Zone: "building-b"},
		},
	})
	require.NoError(t, err)
}

func TestValidateFilterBuildings_ZoneKeys_AllScoped(t *testing.T) {
	store := newOwnedStore(7, 9)
	err := callValidate(t, store, &interfaces.DeviceSetFilter{
		ZoneKeys: []interfaces.ZoneKey{
			{BuildingID: 7, Zone: "Room 2"},
			{BuildingID: 9, Zone: "Room 2"},
		},
	})
	require.NoError(t, err)
}

func TestValidateFilterBuildings_ZoneKeys_MixedScopedAndWildcard(t *testing.T) {
	store := newOwnedStore(7)
	err := callValidate(t, store, &interfaces.DeviceSetFilter{
		ZoneKeys: []interfaces.ZoneKey{
			{BuildingID: 7, Zone: "Room 2"},
			{BuildingID: 0, Zone: "Other Zone"},
		},
	})
	require.NoError(t, err)
}

func TestValidateFilterBuildings_ZoneKeys_RejectsNegativeBuildingID(t *testing.T) {
	err := callValidate(t, nil, &interfaces.DeviceSetFilter{
		ZoneKeys: []interfaces.ZoneKey{
			{BuildingID: -1, Zone: "Room 2"},
		},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "zone_keys")
}

func TestValidateFilterBuildings_ZoneKeys_RejectsEmptyZone(t *testing.T) {
	err := callValidate(t, nil, &interfaces.DeviceSetFilter{
		ZoneKeys: []interfaces.ZoneKey{
			{BuildingID: 7, Zone: ""},
		},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "zone")
}

func TestValidateFilterBuildings_ZoneKeys_RejectsCrossOrgScoped(t *testing.T) {
	// 99 not in caller's org. Error must be scrubbed of the rejected ID.
	store := newOwnedStore(7)
	err := callValidate(t, store, &interfaces.DeviceSetFilter{
		ZoneKeys: []interfaces.ZoneKey{
			{BuildingID: 7, Zone: "Room 2"},
			{BuildingID: 99, Zone: "Cold Aisle"},
		},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "building_ids")
	assert.NotContains(t, err.Error(), "99")
}

func TestValidateFilterBuildings_ZoneKeys_WildcardSkipsCrossOrgCheck(t *testing.T) {
	// All-wildcard request: store never consulted, nil store is fine.
	err := callValidate(t, nil, &interfaces.DeviceSetFilter{
		ZoneKeys: []interfaces.ZoneKey{
			{BuildingID: 0, Zone: "Room 2"},
		},
	})
	require.NoError(t, err)
}

func TestValidateFilterBuildings_BuildingAndZoneCombined(t *testing.T) {
	// building_ids + scoped zone_keys + wildcard zone_keys all carrying
	// the same scoped building id — the bulk lookup dedupes, so a store
	// owning just 7 satisfies the check.
	store := newOwnedStore(7)
	err := callValidate(t, store, &interfaces.DeviceSetFilter{
		BuildingIDs: []int64{7},
		ZoneKeys: []interfaces.ZoneKey{
			{BuildingID: 7, Zone: "Room 2"},
			{BuildingID: 0, Zone: "Wildcard Zone"},
		},
	})
	require.NoError(t, err)
}
