package hostedaccounts_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAccountMinerAttentionUsesAllocatedObservations(t *testing.T) {
	for _, test := range []struct {
		name, status, pairing, want                                    string
		priorPairing, priorStatus, hasError, priorError, crossingError bool
	}{
		{name: "current authentication failure", status: "ACTIVE", pairing: "AUTHENTICATION_NEEDED", want: "attention"},
		{name: "authentication overrides sleeping", status: "INACTIVE", pairing: "AUTHENTICATION_NEEDED", want: "attention"},
		{name: "old authentication failure remains private", status: "ACTIVE", pairing: "AUTHENTICATION_NEEDED", priorPairing: true, want: "hashing"},
		{name: "authentication has its own observation time", status: "ACTIVE", pairing: "AUTHENTICATION_NEEDED", priorStatus: true, want: "attention"},
		{name: "current open error", status: "ACTIVE", hasError: true, want: "attention"},
		{name: "sleeping outranks hardware error", status: "INACTIVE", hasError: true, want: "sleeping"},
		{name: "old open error remains private", status: "ACTIVE", hasError: true, priorError: true, want: "hashing"},
		{name: "crossing error window remains private", status: "ACTIVE", hasError: true, crossingError: true, want: "hashing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, client.AccountID, f.host, hostedaccounts.RoleViewer))
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			if test.priorPairing {
				_, err := f.db.ExecContext(t.Context(), `UPDATE device_pairing SET pairing_status=$1 WHERE device_id=$2`, test.pairing, miner)
				require.NoError(t, err)
			}
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
			var boundary time.Time
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE asset_id=$1 AND kind=5 AND client_id=$2`, miner, client.ID).Scan(&boundary))
			before := boundary.Add(-time.Minute)
			observed := time.Now().UTC()
			statusAt := observed
			if test.priorStatus {
				statusAt = before
			}
			_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_status(device_id,status,status_timestamp) VALUES($1,$2,$3)`, miner, test.status, statusAt)
			require.NoError(t, err)
			if test.pairing != "" && !test.priorPairing {
				_, err = f.db.ExecContext(t.Context(), `UPDATE device_pairing SET pairing_status=$1 WHERE device_id=$2`, test.pairing, miner)
				require.NoError(t, err)
			}
			if test.hasError {
				first, last := observed, observed
				if test.priorError {
					first, last = before, before
				}
				if test.crossingError {
					first = before
				}
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO errors(error_id,org_id,device_id,miner_error,severity,summary,first_seen_at,last_seen_at) VALUES('attention-test',$1,$2,1,1,'Hardware failure',$3,$4)`, f.org, miner, first, last)
				require.NoError(t, err)
			}
			for _, actor := range []int64{f.user, f.host} {
				detail, err := f.svc.AssetDetail(t.Context(), actor, f.org, &pb.GetAccountAssetDetailRequest{AccountId: client.AccountID, Asset: &pb.MiningAssetReference{Kind: 5, Id: miner}})
				require.NoError(t, err)
				require.Equal(t, test.want, detail.Asset.State, "selected-asset query")
				page, err := f.svc.ListAssets(t.Context(), actor, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
				require.NoError(t, err)
				require.Len(t, page.Assets, 1)
				require.Equal(t, test.want, page.Assets[0].State, "paginated query")
				summary, err := f.svc.MiningSummary(t.Context(), actor, f.org, client.AccountID)
				require.NoError(t, err)
				if test.want == "attention" {
					require.EqualValues(t, 1, summary.Totals.AttentionCount)
				}
			}
		})
	}
}
