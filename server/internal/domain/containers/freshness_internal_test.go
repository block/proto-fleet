package containers

import (
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestSummaryExpiresSourceHealthWithoutAnotherPoll(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 2, 0, 0, time.UTC)
	tank := sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{
		{Position: 1, State: sdk.ModuleHashing, ObservedAt: now.Add(-119 * time.Second)},
		{Position: 2, State: sdk.ModuleAttention, ObservedAt: now},
	}}
	first := summarize(tank, now)
	require.EqualValues(t, 1, first.HashingCount)
	later := summarize(tank, now.Add(2*time.Second))
	require.Zero(t, later.HashingCount)
	require.EqualValues(t, 1, later.UnknownCount)
	require.EqualValues(t, 1, later.AttentionCount)
	for _, stamp := range []time.Time{{}, now.Add(time.Hour)} {
		tank.Modules[0].ObservedAt = stamp
		require.EqualValues(t, 1, summarize(tank, now).UnknownCount)
	}
}
