package hostedaccounts_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

// allocateAtSite gives the account one miner located at a new site.
func (f fixture) allocateAtSite(t *testing.T, account, slug, state string) int64 {
	t.Helper()
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site (name,org_id,slug,country,location_state) VALUES ($1,$2,$1,'US',$3) RETURNING id`, slug, f.org, state).Scan(&site))
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, account, 1, selected(5, miner, 0)))
	return site
}

func TestReportingAssumesDefaultsForAccountsWithoutSavedInputs(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Assumed client", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, account.AccountID)
	chicago, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, chicago)
	opts := hostedaccounts.DraftReportOptions{Period: reporting.Period{Kind: "monthly", Start: start, End: start.AddDate(0, 1, 0), AsOf: time.Now().UTC(), Timezone: "America/Chicago"}, OwnerUserID: owner}

	workspace, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.Nil(t, workspace.Input, "no allocated site means no timezone to assume")
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.ErrorContains(t, err, "choose a reporting timezone")

	f.allocateAtSite(t, account.AccountID, "dallas", "TX")
	workspace, err = f.svc.HostReportingSetup(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.NotNil(t, workspace.Input)
	require.Zero(t, workspace.Input.Revision, "assumed defaults are not saved by reading them")
	require.Equal(t, reporting.Configuration{Timezone: "America/Chicago", ReportingYearMonth: 1, ReportingYearDay: 1}, workspace.Input.Configuration)

	report, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	require.EqualValues(t, 1, report.InputRevision)
	workspace, err = f.svc.HostReportingSetup(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 1, workspace.Input.Revision)
	require.Equal(t, "Fleet reporting settings", workspace.Input.SourceRef)
	require.Equal(t, "America/Chicago", workspace.Input.Configuration.Timezone)
	var assumed bool
	var revision, previous int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT (metadata->>'assumed')::boolean,(metadata->>'revision')::bigint,(metadata->>'previous_revision')::bigint FROM activity_log WHERE event_type='save_reporting_inputs'`).Scan(&assumed, &revision, &previous))
	require.True(t, assumed)
	require.EqualValues(t, 1, revision)
	require.Zero(t, previous)

	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.ErrorContains(t, err, "reporting history changed", "revision zero is stale once inputs exist")
}

func TestReportingAssumesNoTimezoneWhenSitesDisagree(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Two zone client", f.user, hostedaccounts.AllCapabilities)
	reportActors(t, f, account.AccountID)
	f.allocateAtSite(t, account.AccountID, "dallas", "TX")
	f.allocateAtSite(t, account.AccountID, "denver", "CO")
	workspace, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.Nil(t, workspace.Input)
}

func TestReportingAssumedInputsRollBackWithTheReport(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Rollback client", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, account.AccountID)
	f.allocateAtSite(t, account.AccountID, "dallas", "TX")
	denver, err := time.LoadLocation("America/Denver")
	require.NoError(t, err)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, denver)
	opts := hostedaccounts.DraftReportOptions{Period: reporting.Period{Kind: "monthly", Start: start, End: start.AddDate(0, 1, 0), AsOf: time.Now().UTC(), Timezone: "America/Denver"}, OwnerUserID: owner}
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.ErrorContains(t, err, "reporting history changed", "a period prepared against another assumed timezone must refresh")

	chicago, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	start = time.Date(2026, 9, 1, 0, 0, 0, 0, chicago)
	opts.Period = reporting.Period{Kind: "monthly", Start: start, End: start.AddDate(0, 1, 0), AsOf: time.Now().UTC(), Timezone: "America/Chicago"}
	_, err = f.db.ExecContext(t.Context(), `CREATE FUNCTION fail_reporting_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='save_reporting_inputs' THEN RAISE EXCEPTION 'reporting audit failed'; END IF; RETURN NEW; END $$; CREATE TRIGGER reporting_audit_failure BEFORE INSERT ON activity_log FOR EACH ROW EXECUTE FUNCTION fail_reporting_audit()`)
	require.NoError(t, err)
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.Error(t, err)
	var inputs, reports int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_reporting_inputs`).Scan(&inputs))
	require.Zero(t, inputs)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report`).Scan(&reports))
	require.Zero(t, reports)
}

func TestReportingAssumesNoTimezoneFromSitesOutsideTheGrant(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Restricted client", f.user, hostedaccounts.AllCapabilities)
	allocated := f.allocateAtSite(t, account.AccountID, "dallas", "TX")
	var other int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site (name,org_id,slug,country,location_state) VALUES ('austin',$1,'austin','US','TX') RETURNING id`, f.org).Scan(&other))
	var host string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&host))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.ReportingAuthority{UserID: host, CanCreate: true, SiteIDs: []int64{other}}, 0))
	workspace, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.Nil(t, workspace.Input, "the allocated site is outside the host's grant")
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.ReportingAuthority{UserID: host, CanCreate: true, SiteIDs: []int64{allocated}}, 1))
	workspace, err = f.svc.HostReportingSetup(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.NotNil(t, workspace.Input)
	require.Equal(t, "America/Chicago", workspace.Input.Configuration.Timezone)
}

func TestReportingSourceReferenceIsRequiredOnlyForEvidence(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Settings client", f.user, hostedaccounts.AllCapabilities)
	reportActors(t, f, account.AccountID)
	input := reportingInputs(t, f, account.AccountID, f.host)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "", input)
	require.NoError(t, err)
	require.Equal(t, "Fleet reporting settings", saved.SourceRef)
	order := reporting.Order{ID: "order-1", Name: "Order", Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), SourceRef: "signed order"}
	_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "", nil, []reporting.FactMutation{reportingUpsertForTest(t, order)})
	require.ErrorContains(t, err, "require a source reference", "register changes always need a reference")

	input.Configuration.ContractRef = "MSA-12"
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, saved.Revision, " ", input)
	require.ErrorContains(t, err, "require a source reference")
	saved, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, saved.Revision, "signed agreement", input)
	require.NoError(t, err)
	require.Equal(t, "signed agreement", saved.SourceRef)
}
