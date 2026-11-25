package sdk

import (
	"testing"

	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestModuleIssueSeverityStringContract(t *testing.T) {
	for name, message := range map[string]proto.Message{
		"plugin":    &pb.ContainerModuleIssue{},
		"inventory": &fleetpb.ModuleIssue{},
	} {
		for _, severity := range []string{"fault", "warning", "observation"} {
			t.Run(name+"/"+severity, func(t *testing.T) {
				// Existing producers encode severity as a string at field 4.
				wire := protowire.AppendString(protowire.AppendTag(nil, 4, protowire.BytesType), severity)
				require.NoError(t, proto.Unmarshal(wire, message))
				encoded, err := protojson.Marshal(message)
				require.NoError(t, err)
				require.JSONEq(t, `{"severity":"`+severity+`"}`, string(encoded))
				require.NoError(t, protojson.Unmarshal(encoded, message))
				roundTrip, err := proto.Marshal(message)
				require.NoError(t, err)
				require.Equal(t, wire, roundTrip)
			})
		}
	}
}
