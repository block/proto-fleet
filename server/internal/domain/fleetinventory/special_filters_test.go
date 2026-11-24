package fleetinventory

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/stretchr/testify/require"
)

func TestSpecialFiltersCanonicalStates(t *testing.T) {
	states := []string{"active", "inactive", "offline", "error", "maintenance", "updating", "needs_mining_pool", "reboot_required", "unknown"}
	service := &Service{}
	for _, n := range []int{8, 9} {
		_, err := service.specialFilters(t.Context(), 1, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, States: states[:n]})
		require.NoError(t, err)
	}
	for _, invalid := range [][]string{{"hashing"}, {"invalid"}, append(append([]string{}, states...), "active")} {
		_, err := service.specialFilters(t.Context(), 1, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, States: invalid})
		require.Error(t, err)
	}
}
