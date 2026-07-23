package reconciler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	minerinterfaces "github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
	"github.com/block/proto-fleet/server/sdk/v1"
)

type fakePoolReferences struct {
	refs map[int64]models.MinerChannelPoolReference
}

func (f *fakePoolReferences) GetMinerChannelPoolReference(_ context.Context, _, poolID int64) (models.MinerChannelPoolReference, error) {
	return f.refs[poolID], nil
}

type fakePoolCapabilities struct{ caps sdk.Capabilities }

func (f fakePoolCapabilities) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return f.caps
}

func TestPoolAdapterSupportedRequiresReadAndWrite(t *testing.T) {
	candidate := models.ConfigEnforcementCandidate{DriverName: "driver"}
	for _, test := range []struct {
		name string
		caps sdk.Capabilities
		want bool
	}{
		{name: "both", caps: sdk.Capabilities{sdk.CapabilityGetMiningPools: true, sdk.CapabilityPoolConfig: true}, want: true},
		{name: "read only", caps: sdk.Capabilities{sdk.CapabilityGetMiningPools: true}, want: false},
		{name: "write only", caps: sdk.Capabilities{sdk.CapabilityPoolConfig: true}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := NewPoolAdapter(nil, nil, fakePoolCapabilities{caps: test.caps}, nil)
			require.Equal(t, test.want, adapter.Supported(context.Background(), candidate))
		})
	}
}

func TestNormalizeObservedPoolsSortsAndComparesCompleteSet(t *testing.T) {
	ordered := normalizeObservedPools([]minerinterfaces.MinerConfiguredPool{
		{Priority: 2, URL: " stratum+tcp://third.example:3333 ", Username: " wallet.third "},
		{Priority: 0, URL: "stratum+tcp://primary.example:3333", Username: "wallet.worker"},
	})
	require.Equal(t, []normalizedPool{
		{Priority: 0, URL: "stratum+tcp://primary.example:3333", Username: "wallet.worker"},
		{Priority: 2, URL: "stratum+tcp://third.example:3333", Username: "wallet.third"},
	}, ordered)
	require.NotEqual(t, hashJSON(ordered), hashJSON(ordered[:1]), "an unexpected extra pool must count as drift")
}

func TestPoolDesiredHashesWorkerAndPoolRevision(t *testing.T) {
	updatedAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	provider := &fakePoolReferences{refs: map[int64]models.MinerChannelPoolReference{
		10: {ID: 10, URL: " stratum+tcp://pool.example:3333 ", Username: "wallet", UpdatedAt: updatedAt},
	}}
	adapter := NewPoolAdapter(provider, nil, nil, nil)
	candidate := models.ConfigEnforcementCandidate{
		OrgID: 1, WorkerName: "rig-7",
		DesiredConfig: &models.MinerChannelDesiredConfig{Pools: &models.MinerChannelPoolDesiredConfig{PrimaryPoolID: 10}},
	}

	first, err := adapter.Desired(context.Background(), candidate)
	require.NoError(t, err)
	require.Equal(t, hashJSON([]normalizedPool{{Priority: 0, URL: "stratum+tcp://pool.example:3333", Username: "wallet.rig-7"}}), first.ComparableHash)

	provider.refs[10] = models.MinerChannelPoolReference{ID: 10, URL: "stratum+tcp://pool.example:3333", Username: "wallet", UpdatedAt: updatedAt.Add(time.Second)}
	passwordOnlyEdit, err := adapter.Desired(context.Background(), candidate)
	require.NoError(t, err)
	require.Equal(t, first.ComparableHash, passwordOnlyEdit.ComparableHash)
	require.NotEqual(t, first.RevisionHash, passwordOnlyEdit.RevisionHash)

	candidate.WorkerName = "rig-8"
	workerChange, err := adapter.Desired(context.Background(), candidate)
	require.NoError(t, err)
	require.NotEqual(t, passwordOnlyEdit.ComparableHash, workerChange.ComparableHash)
	require.NotEqual(t, passwordOnlyEdit.RevisionHash, workerChange.RevisionHash)
}

func TestPoolDesiredPreservesSparseBackupPriority(t *testing.T) {
	updatedAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	provider := &fakePoolReferences{refs: map[int64]models.MinerChannelPoolReference{
		10: {ID: 10, URL: "stratum+tcp://primary.example:3333", Username: "primary.worker", UpdatedAt: updatedAt},
		30: {ID: 30, URL: "stratum+tcp://backup-2.example:3333", Username: "backup.worker", UpdatedAt: updatedAt},
	}}
	backup2 := int64(30)
	adapter := NewPoolAdapter(provider, nil, nil, nil)
	candidate := models.ConfigEnforcementCandidate{
		OrgID: 1,
		DesiredConfig: &models.MinerChannelDesiredConfig{Pools: &models.MinerChannelPoolDesiredConfig{
			PrimaryPoolID: 10,
			Backup2PoolID: &backup2,
		}},
	}

	desired, err := adapter.Desired(context.Background(), candidate)
	require.NoError(t, err)
	require.Equal(t, hashJSON([]normalizedPool{
		{Priority: 0, URL: "stratum+tcp://primary.example:3333", Username: "primary.worker"},
		{Priority: 2, URL: "stratum+tcp://backup-2.example:3333", Username: "backup.worker"},
	}), desired.ComparableHash)
}
