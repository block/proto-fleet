package hostedaccounts_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAccountAnalysisEnergySelectsSiteAndPeriodBeforePayloadBudget(t *testing.T) {
	f, account, _, inputs, p := reportContextFixture(t)
	c := inputs.Coverage[0]
	other := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var otherSite int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Other energy site','other-energy') RETURNING id`, f.org).Scan(&otherSite))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, otherSite, other)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, 1, selected(5, other, 0)))
	var otherInterval int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE asset_id=$2 AND kind=5 RETURNING id`, c.Start, other).Scan(&otherInterval))
	inputs.Coverage = append(inputs.Coverage, reporting.CoverageSegment{ID: "other-site", AllocationIntervalID: otherInterval, Kind: 5, AssetID: other, SiteID: otherSite, ContainerID: other, OrderID: c.OrderID, Start: c.Start, CapacityTHS: "100", SourceRef: "other commercial evidence"})
	inputs.Configuration.Prerequisites = []reporting.Prerequisite{{Key: "G3", Ready: true, Owner: "Meter owner", SourceRef: "verified meter"}}
	inputs.Configuration.Sources = []reporting.Source{{ID: "meter", Metric: "meter", Certified: true, SourceRef: "certificate"}}
	inputs.Meters = []reporting.MeterInterval{{ID: "selected", MeterID: "visible", SourceID: "meter", SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.Start, End: p.End, KWh: "7", Quality: "measured", SourceRef: "reviewed selected interval"}}
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "energy sources", inputs)
	require.NoError(t, err)
	revision := saved.Revision
	const otherRows, historicalRows = 10001, 201
	for offset := 0; offset < otherRows+historicalRows; offset += 200 {
		changes := []reporting.FactMutation{}
		for i := offset; i < min(offset+200, otherRows+historicalRows); i++ {
			m := reporting.MeterInterval{ID: fmt.Sprintf("irrelevant-%05d", i), MeterID: fmt.Sprintf("private-%05d-%s", i, strings.Repeat("m", 1800)), SourceID: "meter", SiteID: otherSite, ContainerID: other, OrderID: c.OrderID, Start: p.Start, End: p.End, KWh: "999", Quality: "measured", SourceRef: strings.Repeat("s", 1900)}
			if i >= otherRows {
				m.SiteID, m.ContainerID = c.SiteID, c.ContainerID
				m.Start, m.End = p.Start.AddDate(-1, 0, 0), p.Start.AddDate(-1, 0, 0).Add(time.Hour)
			}
			changes = append(changes, reportingUpsertForTest(t, m))
		}
		updated, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "retained energy evidence", nil, changes)
		require.NoError(t, err)
		revision = updated.Revision
	}
	q := sqlc.New(f.db)
	args := sqlc.AccountAnalysisEnergyContextSizeParams{OrganizationID: f.org, AccountID: account.ID, SiteFilter: c.SiteID, HostAuthority: []byte("[]"), PeriodStart: p.Start, PeriodEnd: p.End, StartSeconds: p.Start.Unix(), StartNanos: int64(p.Start.Nanosecond()), EndSeconds: p.End.Unix(), EndNanos: int64(p.End.Nanosecond())}
	size, err := q.AccountAnalysisEnergyContextSize(t.Context(), args)
	require.NoError(t, err)
	require.EqualValues(t, 3, size.FactCount, "only selected-site order, coverage and current meter are budgeted")
	require.Less(t, size.PayloadBytes, int64(10000))
	args.SiteFilter = 0
	unfiltered, err := q.AccountAnalysisEnergyContextSize(t.Context(), args)
	require.NoError(t, err)
	require.Greater(t, unfiltered.PayloadBytes, int64(32<<20), "a whole-account context would exhaust the selected input budget")
	require.EqualValues(t, otherRows+4, unfiltered.FactCount, "old same-site history never enters the current period")
	args.SiteFilter = c.SiteID
	args.PeriodStart = p.Start.AddDate(-1, 0, 0)
	args.StartSeconds = args.PeriodStart.Unix()
	history, err := q.AccountAnalysisEnergyContextSize(t.Context(), args)
	require.NoError(t, err)
	require.EqualValues(t, historicalRows+3, history.FactCount)
	var count, stored int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*),sum(payload_bytes) FROM hosted_reporting_fact WHERE account_id=$1`, account.ID).Scan(&count, &stored))
	require.Greater(t, count, int64(10000))
	require.Greater(t, stored, int64(32<<20))

	ctx := analysisContext(t, f, f.user, account.AccountID)
	_, err = f.svc.ReportingInputsMetadata(ctx, f.user, f.org, account.AccountID)
	require.Error(t, err, "customer analysis does not acquire host reporting authority")
	request := analysisRequest(account.AccountID, "energy_usage")
	request.Start, request.End = timestamppb.New(p.Start), timestamppb.New(p.End)
	request.SiteId = &c.SiteID
	generated, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err)
	require.Equal(t, "7", generated.Tables[0].Rows[0].Cells[1])
	require.Len(t, generated.Tables[1].Rows, 1)
	require.Equal(t, []int64{c.SiteID}, generated.SiteIds)
	require.NotContains(t, generated.String(), "999")
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, account.AccountID, true, []int64{c.SiteID}))
	request.SiteId, request.RequestId = nil, uuid.NewString()
	restricted, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err, "excluded retained sites do not impose account-wide host reporting admission")
	require.Equal(t, "7", restricted.Tables[0].Rows[0].Cells[1])
	require.Len(t, restricted.Tables[1].Rows, 1)
	require.Equal(t, []int64{c.SiteID}, restricted.SiteIds)
	t.Logf("energy selected %d facts / %d bytes from %d retained facts / %d bytes", size.FactCount, size.PayloadBytes, count, stored)
}

func TestAccountAnalysisEnergyAndCadenceSelectionPreserveExactBoundaries(t *testing.T) {
	f, account, _, input, p := reportContextFixture(t)
	c := input.Coverage[0]
	c.End = p.Start.Add(200 * time.Nanosecond)
	input.Coverage[0] = c
	input.Meters = []reporting.MeterInterval{{ID: "boundary-meter", MeterID: "meter", SourceID: "meter", SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.Start.Add(-100 * time.Nanosecond), End: p.Start.Add(100 * time.Nanosecond), KWh: "1", Quality: "measured", SourceRef: "exact interval"}}
	_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "exact boundaries", input)
	require.NoError(t, err)
	q := sqlc.New(f.db)
	for _, tc := range []struct {
		offset time.Duration
		facts  int64
	}{
		{50 * time.Nanosecond, 3},
		{100 * time.Nanosecond, 2},
		{200 * time.Nanosecond, 0},
	} {
		start := p.Start.Add(tc.offset)
		args := sqlc.AccountAnalysisEnergyContextSizeParams{OrganizationID: f.org, AccountID: account.ID, SiteFilter: c.SiteID, HostAuthority: []byte("[]"), PeriodStart: start, PeriodEnd: p.End, StartSeconds: start.Unix(), StartNanos: int64(start.Nanosecond()), EndSeconds: p.End.Unix(), EndNanos: int64(p.End.Nanosecond())}
		size, err := q.AccountAnalysisEnergyContextSize(t.Context(), args)
		require.NoError(t, err)
		require.Equal(t, tc.facts, size.FactCount, "original nanosecond end controls half-open selection")
		covered, err := q.ReportingPeriodHasCoverage(t.Context(), sqlc.ReportingPeriodHasCoverageParams{OrganizationID: f.org, AccountID: account.ID, PeriodStart: start, PeriodEnd: p.End, StartSeconds: start.Unix(), StartNanos: int64(start.Nanosecond()), EndSeconds: p.End.Unix(), EndNanos: int64(p.End.Nanosecond())})
		require.NoError(t, err)
		require.Equal(t, tc.facts > 0, covered)
	}
	args := sqlc.AccountAnalysisEnergyContextSizeParams{OrganizationID: f.org, AccountID: account.ID, PeriodStart: p.Start, PeriodEnd: p.End, StartSeconds: p.Start.Unix(), EndSeconds: p.End.Unix()}
	for _, authority := range []string{
		`[{"org_wide":false,"sites":[]}]`,
		fmt.Sprintf(`[{"org_wide":true,"sites":[%d]}]`, c.SiteID),
	} {
		args.HostAuthority = []byte(authority)
		size, err := q.AccountAnalysisEnergyContextSize(t.Context(), args)
		require.NoError(t, err)
		require.Zero(t, size.FactCount, "physical read denial is applied before metadata budgets")
	}
	args.HostAuthority = []byte(fmt.Sprintf(`[{"org_wide":false,"sites":[%d]}]`, c.SiteID))
	size, err := q.AccountAnalysisEnergyContextSize(t.Context(), args)
	require.NoError(t, err)
	require.EqualValues(t, 3, size.FactCount)
}
