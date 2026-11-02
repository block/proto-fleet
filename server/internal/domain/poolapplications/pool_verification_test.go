package poolapplications

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/stretchr/testify/require"
)

type verificationMiner struct {
	interfaces.Miner
	runtime []interfaces.MinerConfiguredPool
	stored  []interfaces.MinerStoredPool
}

func (m verificationMiner) GetMiningPools(context.Context) ([]interfaces.MinerConfiguredPool, error) {
	return slices.Clone(m.runtime), nil
}

func (m verificationMiner) GetStoredMiningPools(context.Context) ([]interfaces.MinerStoredPool, error) {
	return slices.Clone(m.stored), nil
}

func TestVerifyPoolsAcceptsOnlyClearedTrailingSlots(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d configured pools", count), func(t *testing.T) {
			desired := []dto.MiningPool{
				{URL: "stratum+tcp://primary.example", Username: "primary.worker", Password: "primary-secret"},
				{Priority: 1, URL: "stratum+tcp://backup.example", Username: "backup.worker", Password: "backup-secret"},
			}[:count]
			runtime := make([]interfaces.MinerConfiguredPool, count)
			for i, pool := range desired {
				runtime[i] = interfaces.MinerConfiguredPool{Priority: int32(i), URL: pool.URL, Username: pool.Username} // #nosec G115 -- fixture has at most two configured pools.
			}
			for _, scenario := range []struct {
				name, state, code string
				change            func([]interfaces.MinerStoredPool) []interfaces.MinerStoredPool
			}{
				{"cleared trailing slots", "verified", "", nil},
				{"no trailing slots", "verified", "", func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool { return p[:count] }},
				{"unordered readback", "verified", "", func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool { slices.Reverse(p); return p }},
				{"unknown trailing password", resultUnverified, codeReadbackUnavailable, func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool { p[count].Password = nil; return p }},
				{"unknown active password", resultUnverified, codeReadbackUnavailable, func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool { p[0].Password = nil; return p }},
				{"leftover URL", resultDifferent, codeSettingsDiffer, func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool {
					p[count].URL = "stratum+tcp://old.example"
					return p
				}},
				{"leftover username", resultDifferent, codeSettingsDiffer, func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool {
					p[count].Username = "old.worker"
					return p
				}},
				{"leftover password", resultDifferent, codeSettingsDiffer, func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool {
					password := "old-secret"
					p[count].Password = &password
					return p
				}},
				{"wrong active password", resultDifferent, codeSettingsDiffer, func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool {
					password := "wrong"
					p[0].Password = &password
					return p
				}},
				{"duplicate priority", resultDifferent, codeSettingsDiffer, func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool { p[count].Priority = 0; return p }},
				{"missing priority", resultDifferent, codeSettingsDiffer, func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool { p[2].Priority = 3; return p }},
				{"negative priority", resultDifferent, codeSettingsDiffer, func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool { p[count].Priority = -1; return p }},
				{"missing active slot", resultDifferent, codeSettingsDiffer, func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool { return p[:count-1] }},
				{"too many slots", resultDifferent, codeSettingsDiffer, func(p []interfaces.MinerStoredPool) []interfaces.MinerStoredPool {
					return append(p, interfaces.MinerStoredPool{Priority: 3, Password: new(string)})
				}},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					stored := make([]interfaces.MinerStoredPool, 3)
					for i := range stored {
						stored[i] = interfaces.MinerStoredPool{Priority: int32(i), Password: new(string)} // #nosec G115 -- fixture has exactly three stored slots.
						if i < len(desired) {
							stored[i].URL, stored[i].Username = desired[i].URL, desired[i].Username
							*stored[i].Password = desired[i].Password
						}
					}
					if scenario.change != nil {
						stored = scenario.change(stored)
					}
					state, code := verifyPools(t.Context(), verificationMiner{runtime: runtime, stored: stored}, desired)
					require.Equal(t, scenario.state, state)
					require.Equal(t, scenario.code, code)
				})
			}
		})
	}
}
