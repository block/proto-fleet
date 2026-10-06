package command

import (
	"testing"

	"github.com/stretchr/testify/assert"

	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestPairingStatusValuesForSelector(t *testing.T) {
	tests := []struct {
		name   string
		filter *pb.DeviceFilter
		want   []string
	}{
		{
			name: "default includes default password targets",
			want: []string{
				string(sqlc.PairingStatusEnumPAIRED),
				string(sqlc.PairingStatusEnumDEFAULTPASSWORD),
			},
		},
		{
			name: "explicit pairing filter is honored",
			filter: &pb.DeviceFilter{
				PairingStatus: []fleetpb.PairingStatus{fleetpb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED},
			},
			want: []string{string(sqlc.PairingStatusEnumAUTHENTICATIONNEEDED)},
		},
		{
			name: "explicit multiple pairing filters are honored",
			filter: &pb.DeviceFilter{
				PairingStatus: []fleetpb.PairingStatus{
					fleetpb.PairingStatus_PAIRING_STATUS_PAIRED,
					fleetpb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
					fleetpb.PairingStatus_PAIRING_STATUS_PAIRED,
				},
			},
			want: []string{
				string(sqlc.PairingStatusEnumPAIRED),
				string(sqlc.PairingStatusEnumDEFAULTPASSWORD),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pairingStatusValuesForSelector(tt.filter))
		})
	}
}
