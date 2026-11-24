package hostedaccounts_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHostedReportAdHocAssociationUsesSameAccountRecordedReceipt(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Ad hoc client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: time.Now().UTC().Add(-time.Hour), SourceRef: "received client request", Requester: "Client representative", Scope: "September readings", OwnerUserID: owner})
	require.NoError(t, err)
	p := reportPeriod()
	p.Kind = "ad_hoc"
	p.RequestRef = request.RequestID
	p.RequestReceivedAt = request.ReceivedAt
	options := hostedaccounts.DraftReportOptions{Period: p, InputRevision: 1, OwnerUserID: owner}
	report, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, options)
	require.NoError(t, err)
	require.Equal(t, request.RequestID, report.Period.RequestRef)
	require.True(t, request.ReceivedAt.Equal(report.Period.RequestReceivedAt))
	options.Period.RequestReceivedAt = request.ReceivedAt.Add(time.Second)
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, options)
	require.ErrorContains(t, err, "receipt must match")
	options.Period = p
	options.Period.RequestRef = uuid.NewString()
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, options)
	require.Error(t, err, "an arbitrary reference cannot stand in for a recorded request")
	other := f.client(t, "Other ad hoc client", f.user, hostedaccounts.AllCapabilities)
	_, otherReviewer := reportActors(t, f, other.AccountID)
	savedReportInputs(t, f, other.AccountID, otherReviewer, reportingInputs(t, f, other.AccountID, f.host), 0)
	options.Period = p
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, other.AccountID, options)
	require.Error(t, err, "request identity cannot cross account boundaries")
}

func TestHostedReportRequestsRequireFullAccountAndSiteAuthority(t *testing.T) {
	for _, narrowing := range []string{"reporting_grant", "site_role", "account_policy"} {
		t.Run(narrowing, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Private request client", f.user, hostedaccounts.AllCapabilities)
			owner, reviewer := reportActors(t, f, client.AccountID)
			request := hostedaccounts.ReportRequest{ReceivedAt: time.Now().UTC().Add(-time.Hour), SourceRef: "request receipt", Requester: "Private requester", Scope: "Account-wide client scope", OwnerUserID: owner}
			receipt, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, request)
			require.NoError(t, err)
			savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
			period := reportPeriod()
			period.Kind = "ad_hoc"
			period.RequestRef = receipt.RequestID
			period.RequestReceivedAt = receipt.ReceivedAt
			report, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: period, InputRevision: 1, OwnerUserID: owner})
			require.NoError(t, err)
			report, err = f.svc.HostReport(t.Context(), reviewer, f.org, client.AccountID, report.ReportID)
			require.NoError(t, err)
			report, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, report.ReportID, report.Revision, "reviewed missing coverage")
			require.NoError(t, err)
			var site int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(name,org_id,slug) VALUES('Scoped site',$1,'scoped-report-site') RETURNING id`, f.org).Scan(&site))
			switch narrowing {
			case "reporting_grant":
				require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: owner, CanCreate: true, SiteIDs: []int64{site}}, 1))
			case "site_role":
				var role int64
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='FIELD_TECH'`, f.org).Scan(&role))
				_, err := sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.host, OrganizationID: f.org, RoleID: role, ScopeType: "site", ScopeID: sql.NullInt64{Int64: site, Valid: true}})
				require.NoError(t, err)
			case "account_policy":
				rootCtx, err := hostedaccounts.WithRequestAccountID(t.Context(), f.root)
				require.NoError(t, err)
				require.NoError(t, f.svc.ConfigureSites(rootCtx, f.host, f.org, client.AccountID, true, []int64{site}))
			}
			rows, err := f.svc.ReportRequests(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequestListOptions{})
			require.NoError(t, err)
			require.Empty(t, rows.Requests, "unscoped request narratives must not bypass site restrictions")
			_, err = f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, request)
			require.Error(t, err, "restricted authority cannot create account-wide request work")
			setup, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, client.AccountID)
			require.NoError(t, err)
			require.False(t, setup.CanReadRequests)
			require.False(t, setup.CanCreateRequests)
			sent := time.Now().UTC()
			err = f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, report.ReportID, hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "agreed channel", Status: "confirmed", SentAt: &sent, SourceRef: "receipt", Evidence: "delivered requested package"})
			require.Error(t, err, "restricted operators cannot fulfill account-wide requests indirectly through report delivery")
			var deliveries int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_delivery WHERE report_internal_id=$1`, report.ID).Scan(&deliveries))
			require.Zero(t, deliveries)
		})
	}
}
