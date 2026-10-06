package hostedaccounts

import (
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestVerifyStoredPoolsRejectsIncompleteOrDifferentConfiguration(t *testing.T) {
	expected := []sdk.MiningPoolConfig{
		{Priority: 0, URL: "stratum+tcp://primary.example:3333", WorkerName: "payout.with.periods.worker", Password: "private-primary"},
		{Priority: 1, URL: "stratum+ssl://backup.example:443", WorkerName: "backup.worker", Password: ""},
	}
	base := func() []interfaces.MinerConfiguredPool {
		primary, empty := "private-primary", ""
		return []interfaces.MinerConfiguredPool{
			{Priority: 0, URL: expected[0].URL, Username: expected[0].WorkerName, Password: &primary},
			{Priority: 1, URL: expected[1].URL, Username: expected[1].WorkerName, Password: &empty},
		}
	}
	require.NoError(t, verifyStoredPools(expected, base()))
	cases := []struct {
		name   string
		change func([]interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool
	}{
		{"hidden primary password", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			p[0].Password = nil
			return p
		}},
		{"unknown empty password", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			p[1].Password = nil
			return p
		}},
		{"changed stored password", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			v := "different-private"
			p[0].Password = &v
			return p
		}},
		{"changed destination", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			p[0].URL = "stratum+tcp://old-owner.example:3333"
			return p
		}},
		{"changed worker", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			p[0].Username = "old-owner.worker"
			return p
		}},
		{"transport changed", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			p[1].URL = "stratum+tcp://backup.example:443"
			return p
		}},
		{"duplicate priority", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool { p[1].Priority = 0; return p }},
		{"negative priority", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			p[1].Priority = -1
			return p
		}},
		{"missing backup", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool { return p[:1] }},
		{"populated extra backup", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			v := "old-private"
			return append(p, interfaces.MinerConfiguredPool{Priority: 2, URL: "stratum+tcp://old.example:3333", Username: "old.worker", Password: &v})
		}},
		{"unknown disabled backup credential", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			return append(p, interfaces.MinerConfiguredPool{Priority: 2})
		}},
		{"extra backup retains worker", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			v := ""
			return append(p, interfaces.MinerConfiguredPool{Priority: 2, Username: "old.worker", Password: &v})
		}},
		{"extra backup retains password", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			v := "old-private"
			return append(p, interfaces.MinerConfiguredPool{Priority: 2, Password: &v})
		}},
		{"too many slots", func(p []interfaces.MinerConfiguredPool) []interfaces.MinerConfiguredPool {
			v := ""
			return append(p, interfaces.MinerConfiguredPool{Priority: 2, Password: &v}, interfaces.MinerConfiguredPool{Priority: 3, Password: &v})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyStoredPools(expected, tc.change(base()))
			require.Error(t, err)
			require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), "private")
			require.NotContains(t, err.Error(), "old-owner")
		})
	}
	// The getter must expose disabled slots, and verification must check them too.
	empty := ""
	require.NoError(t, verifyStoredPools(expected, append(base(), interfaces.MinerConfiguredPool{Priority: 2, Password: &empty})))
	// Read order is irrelevant; explicit firmware priorities must still be exact.
	reversed := base()
	reversed[0], reversed[1] = reversed[1], reversed[0]
	require.NoError(t, verifyStoredPools(expected, reversed))
	require.Error(t, verifyStoredPools(nil, nil), "empty desired state cannot establish a safe destination")
}
