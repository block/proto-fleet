package fleetinventory

import (
	"math"
	"strings"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestFiltersRejectInapplicableKindsAncestorsAndRanges(t *testing.T) {
	scopes := []Scope{{OrgWide: true}}
	for _, req := range []*pb.ListInventoryRequest{
		{View: pb.InventoryView_INVENTORY_VIEW_FACILITIES, Kinds: []pb.EntityKind{pb.EntityKind_ENTITY_KIND_MODULE}},
		{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, Ancestors: []*pb.EntityRef{{Kind: pb.EntityKind_ENTITY_KIND_TANK, Id: 1}}},
		{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, PageSize: 1001},
		{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, TelemetryRanges: []*commonpb.FleetListTelemetryRangeFilter{{Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_HASHRATE_THS}}},
		{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, TelemetryRanges: []*commonpb.FleetListTelemetryRangeFilter{{Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_HASHRATE_THS, Min: wrapperspb.Double(math.NaN())}}},
		{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, TelemetryRanges: []*commonpb.FleetListTelemetryRangeFilter{{Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_HASHRATE_THS, Min: wrapperspb.Double(10), Max: wrapperspb.Double(5)}}},
	} {
		_, err := listParams(1, req, scopes)
		require.Error(t, err)
	}
	p, err := listParams(1, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS}, scopes)
	require.NoError(t, err)
	require.Equal(t, []int32{5, 6}, p.Kinds)
	require.JSONEq(t, `[{"org_wide":true,"sites":[]}]`, string(p.Authority))
	_, err = listParams(1, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS}, nil)
	require.Error(t, err)
}

func TestCursorIsBoundToQueryAndAuthority(t *testing.T) {
	r := &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS}
	key, err := fingerprint(1, r, []Scope{{OrgWide: true}})
	require.NoError(t, err)
	token, err := encodeCursor(pageCursor{Fingerprint: key, Kind: 6, ID: 10})
	require.NoError(t, err)
	_, err = decodeCursor(token, key)
	require.NoError(t, err)
	for _, scopes := range [][]Scope{{{OrgWide: false}}, {{OrgWide: true, Sites: []int64{2}}}} {
		changed, err := fingerprint(1, r, scopes)
		require.NoError(t, err)
		_, err = decodeCursor(token, changed)
		require.Error(t, err)
	}
	r.SearchQuery = "changed"
	changed, err := fingerprint(1, r, []Scope{{OrgWide: true}})
	require.NoError(t, err)
	_, err = decodeCursor(token, changed)
	require.Error(t, err)
}

func TestSearchLimitCountsUnicodeCharacters(t *testing.T) {
	for _, value := range []string{strings.Repeat("界", 255), strings.Repeat("😀", 255)} {
		_, err := listParams(1, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, SearchQuery: value}, []Scope{{OrgWide: true}})
		require.NoError(t, err)
	}
	_, err := listParams(1, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, SearchQuery: strings.Repeat("界", 256)}, []Scope{{OrgWide: true}})
	require.Error(t, err)
}
