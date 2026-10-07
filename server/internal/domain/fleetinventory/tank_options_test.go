package fleetinventory

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/stretchr/testify/require"
)

func TestTankOptionsRejectInvalidPaginationBeforeReading(t *testing.T) {
	service := NewService(nil)
	for _, request := range []*pb.ListTankOptionsRequest{nil, {PageSize: -1}, {PageSize: 1001}, {PageToken: "invalid"}} {
		_, err := service.TankOptions(t.Context(), 1, request, []Scope{{OrgWide: true}})
		require.Error(t, err)
	}
	_, err := service.TankOptions(t.Context(), 1, &pb.ListTankOptionsRequest{}, nil)
	require.Error(t, err, "missing authority must never read the catalog")
}
