package hostedaccounts_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestReportingMinerCoverageRequiresHistoricalDeviceIdentity(t *testing.T) {
	for _, action := range []string{"save"} {
		for _, correct := range []bool{false, true} {
			name := "unrelated_same_site_device"
			if correct {
				name = "historical_miner_after_move"
			}
			t.Run(action+"/"+name, func(t *testing.T) {
				f := setup(t)
				client := f.client(t, "Miner reporting", f.user, hostedaccounts.AllCapabilities)
				_, _ = reportingBoundaryActors(t, f, client.AccountID)
				miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
				other := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
				var site int64
				require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Miner reporting','miner-reporting') RETURNING id`, f.org).Scan(&site))
				_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id IN ($2,$3)`, site, miner, other)
				require.NoError(t, err)
				require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
				var allocation int64
				var start, end time.Time
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id,started_at,clock_timestamp() FROM hosted_account_asset_interval WHERE kind=5 AND asset_id=$1 AND ended_at IS NULL`, miner).Scan(&allocation, &start, &end))
				attributed := other
				if correct {
					attributed = miner
					// Historical attribution remains valid after current placement changes.
					_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=NULL WHERE id=$1`, miner)
					require.NoError(t, err)
				}
				in := reportingInputs(t, f, client.AccountID, f.host)
				in.Orders = []reporting.Order{{ID: "order", Name: "Signed miner order", Start: start, SourceRef: "signed order"}}
				in.Coverage = []reporting.CoverageSegment{{ID: "miner-coverage", AllocationIntervalID: allocation, Kind: 5, AssetID: miner, SiteID: site, ContainerID: attributed, OrderID: "order", Start: start, End: end, CapacityTHS: "100", SourceRef: "miner allocation"}}
				in.Hashrates = []reporting.HashrateInterval{{ID: "miner-hashrate", Kind: 5, AssetID: miner, SiteID: site, ContainerID: attributed, OrderID: "order", Start: start, End: end, HashrateHS: "100000000000000", Complete: true, SourceID: "reviewed-miner-source", SourceRef: "reviewed miner telemetry"}}
				require.NoError(t, in.Validate(), "both cases pass structural validation")
				if action == "save" {
					_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "miner evidence", in)
				}
				if correct {
					require.NoError(t, err)
					return
				}
				require.ErrorContains(t, err, "commercial coverage exceeds its evidenced allocation interval")
				var saved int
				if action == "save" {
					require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_reporting_inputs WHERE account_id=$1`, client.ID).Scan(&saved))
				} else {
				}
				require.Zero(t, saved, "rejection must not persist an input revision")
			})
		}
	}
}
