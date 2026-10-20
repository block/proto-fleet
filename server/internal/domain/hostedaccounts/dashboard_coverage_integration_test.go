package hostedaccounts_test

import (
	"encoding/json"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestAccountModuleHistoryMissingSourcesVetoEfficiency(t *testing.T) {
	for _, test := range []struct {
		name                                                                          string
		missingPower, beforeAllocation, outsideRange, foreignSite, valueWithoutSource bool
	}{
		{name: "missing hashrate"},
		{name: "missing power", missingPower: true},
		{name: "previous owner absence", beforeAllocation: true},
		{name: "absence outside requested range", outsideRange: true},
		{name: "absence from another site", foreignSite: true},
		{name: "source-less values remain excluded", valueWithoutSource: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			module, _ := f.miningModule(t)
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
			end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
			start := end.Add(-time.Hour)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start.Add(10*time.Minute), client.ID)
			require.NoError(t, err)

			insert := func(at time.Time, hash, power *float64, site any, sourceLessValue bool) {
				t.Helper()
				powerCoverage := sdk.MetricCoverage{Expected: 1, TopologyComplete: true}
				if power != nil {
					powerCoverage.Reporting = 1
					powerCoverage.OldestAt, powerCoverage.NewestAt = at, at
				}
				snapshot := sdk.ContainerSnapshot{ObservedAt: at, TopologyComplete: true, Tanks: []sdk.ContainerTank{{Position: 1, TopologyComplete: true, Modules: []sdk.ContainerModule{{
					Position: 1,
					Hashboards: []sdk.ContainerHashboard{
						{Position: 1, Occupancy: "occupied", ObservedAt: at, HashrateHS: hash},
						{Position: 2, Occupancy: "empty"},
						{Position: 3, Occupancy: "empty"},
					},
					Metrics: &sdk.ContainerModuleMetrics{ObservedAt: at, Rollup: &sdk.ContainerRollup{PowerW: power, Power: powerCoverage}},
				}}}}}
				sdk.CalculateContainerRollups(&snapshot)
				metric := snapshot.Tanks[0].Modules[0].Metrics
				if hash == nil {
					require.Zero(t, metric.Rollup.Hashrate.Reporting)
					require.True(t, metric.Rollup.Hashrate.OldestAt.IsZero())
					require.True(t, metric.Rollup.Hashrate.NewestAt.IsZero())
				}
				if power == nil {
					require.Zero(t, metric.Rollup.Power.Reporting)
					require.True(t, metric.Rollup.Power.OldestAt.IsZero())
					require.True(t, metric.Rollup.Power.NewestAt.IsZero())
				}
				if sourceLessValue {
					metric.HashrateHS = new(999e12)
				}
				coverage, err := json.Marshal(metric.Rollup)
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,site_id,hashrate_hs,power_w,coverage) VALUES($1,$2,$2,$3,$4,$5,$6)`, module, at, site, metric.HashrateHS, metric.PowerW, coverage)
				require.NoError(t, err)
			}
			insert(start.Add(20*time.Minute), new(100e12), new(2000.0), nil, false)
			query := func() *pb.MiningSummary {
				result, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, start, end, nil))
				require.NoError(t, err)
				require.Len(t, result.Readings, 1)
				return result.Readings[0].Summary
			}
			require.Equal(t, 20.0, query().GetEfficiencyJTh(), "the complete poll establishes the baseline")
			at := start.Add(30 * time.Minute)
			if test.beforeAllocation {
				at = start.Add(5 * time.Minute)
			}
			if test.outsideRange {
				at = end.Add(30 * time.Second)
			}
			var site any
			if test.foreignSite {
				site = int64(99999)
			}
			if test.missingPower {
				insert(at, new(200e12), nil, site, false)
			} else {
				insert(at, nil, new(4000.0), site, test.valueWithoutSource)
			}
			summary := query()
			if test.beforeAllocation || test.outsideRange || test.foreignSite {
				require.Equal(t, 20.0, summary.GetEfficiencyJTh(), "unauthorized or out-of-range absence cannot affect coverage")
				require.Zero(t, summary.PartialModuleCount)
				require.Equal(t, 100e12, summary.GetHashrateHs())
				require.Equal(t, 2000.0, summary.GetPowerW())
			} else {
				require.Nil(t, summary.EfficiencyJTh, "an authorized poll with no source timestamps must veto efficiency")
				require.EqualValues(t, 1, summary.PartialModuleCount)
				require.EqualValues(t, 1, summary.HashrateReportingCount)
				require.EqualValues(t, 1, summary.PowerReportingCount)
				if test.missingPower {
					require.Equal(t, 150e12, summary.GetHashrateHs())
					require.Equal(t, 2000.0, summary.GetPowerW())
				} else {
					require.Equal(t, 100e12, summary.GetHashrateHs(), "missing source time never authorizes a value")
					require.Equal(t, 3000.0, summary.GetPowerW(), "authorized measured subtotals remain available")
				}
			}
		})
	}
}
