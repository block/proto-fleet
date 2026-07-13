package fleetmanagement

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/generated/sqlc"
	storesMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// TestListMinerStateSnapshots_PopulatesDirectPlacementRefs asserts that the
// snapshot builder propagates direct row-stamped placement refs without a
// second lookup.
func TestListMinerStateSnapshots_PopulatesDirectPlacementRefs(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := storesMocks.NewMockDeviceStore(ctrl)
	svc := &Service{deviceStore: store}

	rows := []sqlc.ListMinerStateSnapshotsRow{
		{
			DeviceIdentifier: "miner-a",
			DriverName:       "antminer",
			PairingStatus:    "UNPAIRED",
			SiteID:           sql.NullInt64{Int64: 7, Valid: true},
			SiteLabel:        "Site Alpha",
			BuildingID:       sql.NullInt64{Int64: 11, Valid: true},
			BuildingLabel:    "Building One",
			CohortID:         19,
			CohortLabel:      "Pilot Cohort",
		},
		{
			DeviceIdentifier: "miner-b",
			DriverName:       "antminer",
			PairingStatus:    "UNPAIRED",
			// Site unset — placement must remain unset.
			SiteID:    sql.NullInt64{},
			SiteLabel: "",
		},
	}
	store.EXPECT().ListMinerStateSnapshots(gomock.Any(), int64(1), "", int32(10), gomock.Any(), gomock.Any()).
		Return(rows, "", int64(len(rows)), nil)

	snaps, _, total, err := svc.buildSnapshotsFromUnifiedQuery(t.Context(), 1, "", 10, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, snaps, 2)

	require.NotNil(t, snaps[0].Placement, "miner-a must surface placement")
	require.NotNil(t, snaps[0].Placement.Site, "miner-a must surface its assigned site ref")
	assert.Equal(t, int64(7), snaps[0].Placement.Site.Id)
	assert.Equal(t, "Site Alpha", snaps[0].Placement.Site.Label)
	require.NotNil(t, snaps[0].Placement.Building, "miner-a must surface its assigned building ref")
	assert.Equal(t, int64(11), snaps[0].Placement.Building.Id)
	assert.Equal(t, "Building One", snaps[0].Placement.Building.Label)
	require.NotNil(t, snaps[0].Placement.Cohort, "miner-a must surface its effective cohort ref")
	assert.Equal(t, int64(19), snaps[0].Placement.Cohort.Id)
	assert.Equal(t, "Pilot Cohort", snaps[0].Placement.Cohort.Label)

	assert.Nil(t, snaps[1].Placement, "unassigned miner must not surface placement")
}
