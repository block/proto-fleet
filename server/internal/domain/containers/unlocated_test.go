package containers_test

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/containers"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/require"
)

func TestProjectUnlocatedHashboards(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name        string
		count       uint32
		metricsAt   time.Time
		observedAt  time.Time
		hasCoverage bool
		want        *uint32
	}{
		{"mixed inventory", 6, now, now, true, new(uint32(6))},
		{"entirely unlocated", 288, now, now, true, new(uint32(288))},
		{"locations resolved", 0, now, now, true, new(uint32(0))},
		{"stale coverage", 6, now.Add(-3 * time.Minute), now, true, nil},
		{"stale inventory", 6, now, now.Add(-3 * time.Minute), true, nil},
		{"future coverage", 6, now.Add(time.Minute), now, true, nil},
		{"missing coverage", 0, now, now, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(sdk.ContainerRollup{UnlocatedHashboards: tc.count})
			require.NoError(t, err)
			rows, err := containers.Project([]sqlc.ListContainersRow{{ID: 1, DeviceIdentifier: "ct1", ObservedAt: sql.NullTime{Time: tc.observedAt, Valid: true}, TankHealth: []byte(`[]`), MetricsAt: tc.metricsAt, Coverage: pqtype.NullRawMessage{RawMessage: data, Valid: tc.hasCoverage}}}, func(*int64) (bool, error) { return true, nil })
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, tc.want, rows[0].UnlocatedHashboards)
		})
	}
}

func TestUnlocatedInventoryAuthorizationPrecedesProjection(t *testing.T) {
	var checkedSite *int64
	rows, err := containers.Project([]sqlc.ListContainersRow{{
		ID: 1, DeviceIdentifier: "private-ct1", SiteID: sql.NullInt64{Int64: 42, Valid: true},
		UnlocatedBoards: json.RawMessage(`invalid unlocated inventory`),
	}}, func(siteID *int64) (bool, error) {
		checkedSite = siteID
		return false, nil
	})
	require.NoError(t, err, "denied inventory must not be decoded or exposed")
	require.Equal(t, new(int64(42)), checkedSite)
	require.Empty(t, rows)
}
