package command

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"

	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
)

func TestPairingStatusFiltersForSelector(t *testing.T) {
	tests := []struct {
		name        string
		filter      *pb.DeviceFilter
		commandType commandtype.Type
		want        []sql.NullString
	}{
		{
			name:        "normal commands keep query default",
			commandType: commandtype.Reboot,
			want:        []sql.NullString{{}},
		},
		{
			name:        "password update includes default password remediation targets",
			commandType: commandtype.UpdateMinerPassword,
			want: []sql.NullString{
				{String: string(sqlc.PairingStatusEnumPAIRED), Valid: true},
				{String: string(sqlc.PairingStatusEnumDEFAULTPASSWORD), Valid: true},
			},
		},
		{
			name: "explicit pairing filter is honored",
			filter: &pb.DeviceFilter{
				PairingStatus: []fleetpb.PairingStatus{fleetpb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED},
			},
			commandType: commandtype.UpdateMinerPassword,
			want: []sql.NullString{{
				String: string(sqlc.PairingStatusEnumAUTHENTICATIONNEEDED),
				Valid:  true,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pairingStatusFiltersForSelector(tt.filter, tt.commandType))
		})
	}
}
