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

// TestListMinerStateSnapshots_PopulatesSiteIDAndLabel asserts that the
// snapshot builder propagates the row-stamped site_id + site_label to
// the proto. Plan §"device/" snapshot writer audit — every snapshot
// construction must surface the assigned site without a second lookup.
func TestListMinerStateSnapshots_PopulatesSiteIDAndLabel(t *testing.T) {
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
		},
		{
			DeviceIdentifier: "miner-b",
			DriverName:       "antminer",
			PairingStatus:    "UNPAIRED",
			// Site unset — snapshot.SiteId must remain nil and label empty.
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

	require.NotNil(t, snaps[0].SiteId, "miner-a must surface its assigned site_id")
	assert.Equal(t, int64(7), *snaps[0].SiteId)
	assert.Equal(t, "Site Alpha", snaps[0].SiteLabel)

	assert.Nil(t, snaps[1].SiteId, "unassigned miner must not surface a site_id")
	assert.Empty(t, snaps[1].SiteLabel)
}
