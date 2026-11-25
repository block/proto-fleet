package hostedaccounts_test

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestHostedReportPublicationRetainsHostRawMeterEvidenceAndFreezesClientShare(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Shared meter client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	p := reportPeriod()
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Meter site','meter-site') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	historyEpoch(t, f, p.Start)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, p.Start, client.ID)
	require.NoError(t, err)
	var interval int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE client_id=$1`, client.ID).Scan(&interval))
	input := reportingInputs(t, f, client.AccountID, f.host)
	input.Configuration.SharedAllocationRef = "reviewed allocation"
	input.Configuration.Sources = []reporting.Source{{ID: "meter", Metric: "meter", Certified: true, SourceRef: "meter certification"}}
	input.Orders = []reporting.Order{{ID: "order", Name: "Client order", Start: p.Start, SourceRef: "signed order"}}
	input.Coverage = []reporting.CoverageSegment{{ID: "coverage", AllocationIntervalID: interval, Kind: 5, AssetID: miner, SiteID: site, ContainerID: miner, OrderID: "order", Start: p.Start, CapacityTHS: "100", SourceRef: "commercial coverage"}}
	// The private values have more fractional digits than a timestamp, so a
	// substring match in the archive is always a leak and never an issue time.
	input.Meters = []reporting.MeterInterval{{ID: "meter", MeterID: "shared-physical", SourceID: "meter", SiteID: site, ContainerID: miner, OrderID: "order", Start: p.Start, End: p.End, KWh: "98765.4321000000000000001", AllocationFraction: "0.250000000001", Quality: "measured", SourceRef: "approved meter reading"}}
	savedReportInputs(t, f, client.AccountID, reviewer, input, 0)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: 1, OwnerUserID: owner})
	require.NoError(t, err)
	issued, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete source")
	require.NoError(t, err)
	raw, err := reportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID)
	require.NoError(t, err)
	require.Equal(t, input.Meters, raw.Inputs.Meters, "host source evidence must remain unchanged")
	_, err = reportingInputsForTest(f.svc, t.Context(), f.user, f.org, client.AccountID)
	require.Error(t, err, "raw reviewed inputs remain host-only")
	archive := downloadReportArtifact(t, f, f.user, client.AccountID, issued.ReportID, "evidence.zip")
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)
	found := false
	for _, file := range reader.File {
		r, err := file.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		for _, private := range []string{input.Meters[0].KWh, input.Meters[0].AllocationFraction, "allocation_fraction", "allocationFraction"} {
			require.NotContains(t, string(data), private, file.Name)
		}
		if file.Name == "inputs/meters.csv" {
			found = true
			require.Contains(t, string(data), "24691.3580250987654321000250000000001")
		}
	}
	require.True(t, found)
}
