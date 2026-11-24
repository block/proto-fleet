package fleetinventory_test

import (
	"testing"

	"buf.build/go/protovalidate"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/stretchr/testify/require"
)

func TestListInventoryRequestValidation_UnassignedScopes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		request *pb.ListInventoryRequest
		valid   bool
	}{
		{"facilities without unassigned scope", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_FACILITIES}, true},
		{"equipment without unassigned scope", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS}, true},
		{"assets without unassigned scope", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS}, true},
		{"equipment without a facility", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, IncludeNoFacility: true}, true},
		{"assets without a facility", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, IncludeNoFacility: true}, true},
		{"assets without a group", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, IncludeNoGroup: true}, true},
		{"assets without either placement", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, IncludeNoFacility: true, IncludeNoGroup: true}, true},
		{"facilities reject unassigned facility", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_FACILITIES, IncludeNoFacility: true}, false},
		{"facilities reject unassigned group", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_FACILITIES, IncludeNoGroup: true}, false},
		{"equipment rejects unassigned group", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, IncludeNoGroup: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := protovalidate.Validate(tc.request)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "Unassigned facility and group scopes must apply to the requested inventory view")
			}
		})
	}
}
