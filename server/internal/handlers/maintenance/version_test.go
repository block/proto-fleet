package maintenance

import (
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTicketVersionsPreserveTimestampPrecision(t *testing.T) {
	version := time.Unix(1700000000, 123456000)
	params, err := toUpdateParams(&pb.UpdateRepairTicketRequest{Id: 7, ExpectedUpdatedAt: timestamppb.New(version)}, 42)
	require.NoError(t, err)
	require.True(t, version.Equal(params.ExpectedUpdatedAt))
	versions, err := toExpectedVersions([]*pb.TicketVersion{{TicketId: 7, UpdatedAt: timestamppb.New(version)}})
	require.NoError(t, err)
	require.True(t, version.Equal(versions[7]))
}

func TestTicketVersionsRejectMissingMalformedAndDuplicateTokens(t *testing.T) {
	for _, version := range []*timestamppb.Timestamp{nil, {Seconds: 1, Nanos: -1}} {
		_, err := toUpdateParams(&pb.UpdateRepairTicketRequest{Id: 7, ExpectedUpdatedAt: version}, 42)
		require.Error(t, err)
		_, err = toExpectedVersions([]*pb.TicketVersion{{TicketId: 7, UpdatedAt: version}})
		require.Error(t, err)
	}
	version := &pb.TicketVersion{TicketId: 7, UpdatedAt: timestamppb.Now()}
	_, err := toExpectedVersions([]*pb.TicketVersion{version, version})
	require.ErrorContains(t, err, "duplicate")
	_, err = toExpectedVersions([]*pb.TicketVersion{nil})
	require.Error(t, err)
}
