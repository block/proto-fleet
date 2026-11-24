package hostedaccounts_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestReportPreparationBreakdownBudgetDoesNotPersistPartialReport(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Report work budget", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	input := reportingInputs(t, f, client.AccountID, f.host)
	p := reportPeriod()
	for i := range reporting.MaxReportBreakdowns + 1 {
		input.Orders = append(input.Orders, reporting.Order{ID: fmt.Sprint(i), Name: "Order", Start: p.Start, SourceRef: "signed order"})
	}
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), reviewer, f.org, client.AccountID, 0, "reviewed output budget fixture", input)
	require.NoError(t, err)
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner})
	require.ErrorContains(t, err, "1024 supported breakdowns")
	var reports, audits int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report`).Scan(&reports))
	require.Zero(t, reports)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='prepare_report'`).Scan(&audits))
	require.Zero(t, audits)
}

func TestWrittenReportSizeFailureRollsBackPreparation(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Report HTML budget", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	container := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'HTML budget site','html-budget') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, container)
	require.NoError(t, err)
	p := reportPeriod()
	input := reportingInputs(t, f, client.AccountID, f.host)
	input.Orders = []reporting.Order{{ID: "order", Name: "Order", Start: p.Start, SourceRef: "signed order"}}
	for i := range 900 {
		input.Incidents = append(input.Incidents, reporting.Incident{ID: fmt.Sprint(i), SiteID: site, ContainerID: container, OrderID: "order", Start: p.Start, End: p.End, AsOf: p.AsOf, Summary: "Current incident", RootCause: strings.Repeat("\"", 4000), SourceRef: "incident evidence"})
	}
	payload, err := json.Marshal(input)
	require.NoError(t, err)
	require.Less(t, len(payload), 8<<20, "valid input JSON fits while HTML escaping can exceed the written budget")
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), reviewer, f.org, client.AccountID, 0, "reviewed output budget fixture", input)
	require.NoError(t, err)
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner})
	require.ErrorContains(t, err, "supported byte limit")
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	var artifacts, audits int
	var reports int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact`).Scan(&artifacts))
	require.Zero(t, artifacts)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='prepare_report'`).Scan(&audits))
	require.Zero(t, audits)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report`).Scan(&reports))
	require.Zero(t, reports)
}

func TestReportPreparationRejectsCredentialReferencesBeforeRetainingDraft(t *testing.T) {
	for _, field := range []string{"contract", "order"} {
		t.Run(field, func(t *testing.T) {
			f, account, owner, in, p := reportContextFixture(t)
			if field == "contract" {
				in.Configuration.ContractRef = "https://example.test/contract?token=secret"
			} else {
				in.Orders[0].SourceRef = "https://example.test/order?token=secret"
			}
			saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "reviewed inputs", in)
			require.NoError(t, err)
			_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner})
			require.True(t, fleeterror.IsFailedPreconditionError(err))
			require.ErrorContains(t, err, "credential-bearing evidence reference")
			var count int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report`).Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestLegacyOversizedResultCannotBeDecodedOrPublished(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_report SET result=jsonb_build_object('legacyPayload',repeat('x',$1)) WHERE id=$2`, reporting.MaxResultJSONBytes, draft.ID)
	require.NoError(t, err)
	_, err = f.svc.HostReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID)
	require.ErrorContains(t, err, "report result exceeds supported byte limit")
	_, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed limitation")
	require.ErrorContains(t, err, "report result exceeds supported byte limit")
	var artifacts int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact`).Scan(&artifacts))
	require.Zero(t, artifacts)
}
