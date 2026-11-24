package hostedaccounts_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

// The immutable baseline is a genuinely published earlier report. The later
// annual report exercises the service's publication wiring, not just the selector.
func TestHostedReportPublicEvidencePeriodScopeAndFrozenHistory(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Scoped evidence client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Scoped evidence site','scoped-evidence-site') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	prior := start.AddDate(-1, 0, 0)
	end := start.AddDate(1, 0, 0)
	oldest := prior.AddDate(-1, 0, 0)
	historyEpoch(t, f, oldest)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, oldest, client.ID)
	require.NoError(t, err)
	var interval int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE client_id=$1`, client.ID).Scan(&interval))

	input := reportingInputs(t, f, client.AccountID, f.host)
	input.Configuration.SharedAllocationRef = "reviewed shared allocation"
	input.Configuration.Sources = []reporting.Source{{ID: "meter-source", Metric: "meter", Certified: true, SourceRef: "reviewed meter source"}}
	input.Orders = []reporting.Order{{ID: "order", Name: "Client order", Start: oldest, SourceRef: "reviewed order"}}
	input.Coverage = []reporting.CoverageSegment{{ID: "coverage", AllocationIntervalID: interval, Kind: 5, AssetID: miner, SiteID: site, ContainerID: miner, OrderID: "order", Start: oldest, CapacityTHS: "100", SourceRef: "reviewed coverage"}}
	input.Meters = []reporting.MeterInterval{
		{ID: "prior-meter", MeterID: "meter-prior", SourceID: "meter-source", SiteID: site, ContainerID: miner, OrderID: "order", Start: prior, End: start, KWh: "98765.4321", AllocationFraction: "0.25", Quality: "measured", SourceRef: "prior meter evidence"},
		{ID: "current-meter", MeterID: "meter-current", SourceID: "meter-source", SiteID: site, ContainerID: miner, OrderID: "order", Start: start, End: end, KWh: "12345.6789", AllocationFraction: "0.4", Quality: "measured", SourceRef: "current meter evidence"},
		{ID: "crossing-meter", MeterID: "meter-crossing", SourceID: "meter-source", SiteID: site, ContainerID: miner, OrderID: "order", Start: start.Add(-time.Hour), End: start.Add(time.Hour), KWh: "76543.1234", AllocationFraction: "0.2", Quality: "measured", SourceRef: "crossing meter evidence"},
	}
	fee := func(id string, begin, finish time.Time) reporting.FeeLine {
		return reporting.FeeLine{ID: id, SiteID: site, ContainerID: miner, OrderID: "order", Start: begin, End: finish, Amount: "5", Currency: "USD", SourceRef: id + " evidence"}
	}
	input.Fees = []reporting.FeeLine{
		fee("old-fee", oldest.AddDate(0, 1, 0), oldest.AddDate(0, 2, 0)),
		fee("prior-fee", prior.AddDate(0, 1, 0), prior.AddDate(0, 2, 0)),
		fee("current-fee", start.AddDate(0, 1, 0), start.AddDate(0, 2, 0)),
		fee("future-fee", end.AddDate(0, 1, 0), end.AddDate(0, 2, 0)),
	}
	savedReportInputs(t, f, client.AccountID, reviewer, input, 0)
	publish := func(period reporting.Period, revision int64) hostedaccounts.ReportRecord {
		t.Helper()
		draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: period, InputRevision: revision, OwnerUserID: owner})
		require.NoError(t, err)
		issued, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete sources")
		require.NoError(t, err)
		return issued
	}
	asOf := time.Now().UTC()
	oldPeriod := reporting.Period{Kind: "monthly", Start: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC), AsOf: asOf, Timezone: "UTC"}
	old := publish(oldPeriod, 1)
	before := downloadReportArtifact(t, f, f.user, client.AccountID, old.ReportID, "evidence.zip")

	// Force a distinct saved revision before the later publication.
	input.Configuration.DeliveryChannel = "later reviewed configuration"
	savedReportInputs(t, f, client.AccountID, reviewer, input, 1)
	// Capture an hour of monitoring evidence, then freeze it by the draft's cutoff.
	observed := start.AddDate(0, 5, 0)
	reportEvidenceHours(t, f.db, f, interval, observed, 1, 1, 100e12)
	annual := reporting.Period{Kind: "annual", Start: start, End: end, AsOf: reportAsOf(t, f), Timezone: "UTC"}
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: annual, InputRevision: 2, OwnerUserID: owner})
	require.NoError(t, err)
	held := []string{fmt.Sprintf("%d %s closed 1", interval, observed.Format(time.RFC3339))}
	require.Equal(t, held, frozenReportEvidence(t, f, draft.ID))
	// A later revision is captured after the draft's cutoff; the draft must not include it.
	reportEvidenceHours(t, f.db, f, interval, observed, 1, 2, 200e12)
	require.Equal(t, held, frozenReportEvidence(t, f, draft.ID), "post-prepare revisions must not change frozen membership")
	issued, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete sources")
	require.NoError(t, err)
	archive := downloadReportArtifact(t, f, f.user, client.AccountID, issued.ReportID, "evidence.zip")
	require.Equal(t, before, downloadReportArtifact(t, f, f.user, client.AccountID, old.ReportID, "evidence.zip"), "later input and publication must not rewrite the issued artifact")

	members := scopedPublicationMembers(t, archive)
	require.Contains(t, members, "observations.csv")
	observations, err := csv.NewReader(bytes.NewReader(members["observations.csv"])).ReadAll()
	require.NoError(t, err)
	require.Equal(t, []string{"1"}, observationColumn(t, observations, "revision"), "the frozen hour must be present in the public ZIP")
	require.Equal(t, []string{"100000000000000"}, observationColumn(t, observations, "hashrate_sum_hs"), "a later revision must not enter the public ZIP")
	fees, _ := scopedPublicationTable(t, members, "fees")
	require.Contains(t, fees, "current-fee")
	require.NotContains(t, fees, "prior-fee")
	require.NotContains(t, fees, "old-fee")
	require.NotContains(t, fees, "future-fee")
	meters, columns := scopedPublicationTable(t, members, "meters")
	amount, window := slices.Index(columns, "attributed_kwh"), slices.Index(columns, "comparison_window")
	require.NotEqual(t, -1, amount)
	require.NotEqual(t, -1, window)
	require.Contains(t, meters, "prior-meter")
	require.Contains(t, meters, "current-meter")
	require.Equal(t, "24691.358025", meters["prior-meter"][amount])
	require.Equal(t, "prior_year", meters["prior-meter"][window])
	require.Equal(t, "4938.27156", meters["current-meter"][amount])
	require.Equal(t, "current", meters["current-meter"][window])
	if crossing, ok := meters["crossing-meter"]; ok {
		require.Empty(t, crossing[amount], "a crossing meter must not be prorated")
	}
	for name, data := range members {
		for _, private := range []string{"98765.4321", "12345.6789", "76543.1234", "allocation_fraction", "allocationFraction"} {
			require.NotContains(t, string(data), private, name)
		}
	}
}

func scopedPublicationMembers(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)
	members := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		_, exists := members[file.Name]
		require.False(t, exists, "duplicate ZIP member %s", file.Name)
		stream, err := file.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(stream)
		require.NoError(t, err)
		require.NoError(t, stream.Close())
		members[file.Name] = data
	}
	var manifest struct {
		Files []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
			Bytes  int64  `json:"bytes"`
		} `json:"files"`
	}
	require.Contains(t, members, "manifest.json")
	require.NoError(t, json.Unmarshal(members["manifest.json"], &manifest))
	require.Len(t, manifest.Files, len(members)-1)
	seen := make(map[string]bool, len(manifest.Files))
	for _, entry := range manifest.Files {
		require.False(t, seen[entry.Name], "duplicate manifest member %s", entry.Name)
		seen[entry.Name] = true
		data, ok := members[entry.Name]
		require.True(t, ok, "manifest member %s missing", entry.Name)
		digest := sha256.Sum256(data)
		require.Equal(t, hex.EncodeToString(digest[:]), entry.SHA256, entry.Name)
		require.EqualValues(t, len(data), entry.Bytes, entry.Name)
	}
	return members
}

func scopedPublicationTable(t *testing.T, members map[string][]byte, name string) (map[string][]string, []string) {
	t.Helper()
	member := "inputs/" + name + ".csv"
	require.Contains(t, members, member)
	table, err := csv.NewReader(bytes.NewReader(members[member])).ReadAll()
	require.NoError(t, err)
	require.NotEmpty(t, table)
	columns := table[0]
	id := slices.Index(columns, "id")
	require.NotEqual(t, -1, id)
	rows := make(map[string][]string, len(table)-1)
	for _, row := range table[1:] {
		require.Len(t, row, len(columns))
		key := row[id]
		require.NotContains(t, rows, key)
		rows[key] = row
	}
	return rows, columns
}
