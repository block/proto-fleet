package command

import (
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestScopedMiningActivityDescriptionUsesPhysicalCoordinates(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload scopedMiningPayload
		want    string
	}{
		{
			name: "tanks",
			payload: scopedMiningPayload{
				TankMiningRequest: sdk.TankMiningRequest{Mining: new(false), Positions: []int32{1, 3}},
				TankIDs:           []int64{101, 303},
			},
			want: "North tanks [1 3]: mining=false",
		},
		{
			name: "modules",
			payload: scopedMiningPayload{
				TankMiningRequest: sdk.TankMiningRequest{Mining: new(true)},
				ModuleIDs:         []int64{404, 505},
				Modules:           []sdk.ModuleMiningTarget{{Tank: 1, Module: 2, ReportedID: "a"}, {Tank: 3, Module: 2, ReportedID: "b"}},
			},
			want: "North modules [tank 1/module 2, tank 3/module 2]: mining=true",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, test.payload.activityDescription("North"))
		})
	}
}
