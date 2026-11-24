package poolconfigurations

import (
	"testing"

	"buf.build/go/protovalidate"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAssignmentRequiresExplicitRemovalIntent(t *testing.T) {
	id := uuid.NewString()
	for _, tc := range []struct {
		name    string
		request *pb.ReviewAssignmentRequest
		valid   bool
	}{
		{"assignment", &pb.ReviewAssignmentRequest{ConfigurationId: id, ExpectedVersion: 1, DeviceIds: []int64{1}}, true},
		{"missing configuration cannot clear", &pb.ReviewAssignmentRequest{DeviceIds: []int64{1}}, false},
		{"explicit device clear", &pb.ReviewAssignmentRequest{RemovalTarget: &pb.RetryTarget{Kind: "device", TargetId: 1}}, true},
		{"explicit board clear", &pb.ReviewAssignmentRequest{RemovalTarget: &pb.RetryTarget{Kind: "hashboard_installation", TargetId: 1}}, true},
		{"mixed intent", &pb.ReviewAssignmentRequest{ConfigurationId: id, ExpectedVersion: 1, RemovalTarget: &pb.RetryTarget{Kind: "device", TargetId: 1}}, false},
		{"mixed selection", &pb.ReviewAssignmentRequest{DeviceIds: []int64{2}, RemovalTarget: &pb.RetryTarget{Kind: "device", TargetId: 1}}, false},
		{"invalid target", &pb.ReviewAssignmentRequest{RemovalTarget: &pb.RetryTarget{Kind: "device", TargetId: 0}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := protovalidate.Validate(tc.request)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
