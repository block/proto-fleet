package hostedaccounts_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

// Publication must recheck the complete frozen, multi-site scope even when
// the publisher retains access to one site. Use frozen hourly evidence on both
// sites and a genuinely issued earlier archive, rather than a synthetic draft.
func TestHostedReportPublicScopeRevokedAuthorityIsAtomic(t *testing.T) {
	for _, narrowing := range []string{"reporting_site_grant", "host_site_role"} {
		t.Run(narrowing, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Multi-site reporting client", f.user, hostedaccounts.AllCapabilities)
			owner, publisher := reportActors(t, f, client.AccountID)
			var publisherUUID string
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, publisher).Scan(&publisherUUID))

			sites := make([]int64, 2)
			miners := make([]int64, 2)
			for i := range sites {
				require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,$2,$2) RETURNING id`, f.org, fmt.Sprintf("authority-site-%d", i)).Scan(&sites[i]))
				miners[i] = f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
				_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, sites[i], miners[i])
				require.NoError(t, err)
			}
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miners[0], 0), selected(5, miners[1], 0)))
			start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
			historyStart := start.AddDate(0, -2, 0)
			historyEpoch(t, f, historyStart)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, historyStart, client.ID)
			require.NoError(t, err)
			input := reportingInputs(t, f, client.AccountID, f.host)
			input.Orders = []reporting.Order{{ID: "order", Name: "Signed order", Start: historyStart, SourceRef: "order evidence"}}
			allocations := make([]int64, len(miners))
			for i, miner := range miners {
				var allocation int64
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE client_id=$1 AND kind=5 AND asset_id=$2`, client.ID, miner).Scan(&allocation))
				allocations[i] = allocation
				input.Coverage = append(input.Coverage, reporting.CoverageSegment{ID: fmt.Sprintf("coverage-%d", i), AllocationIntervalID: allocation, Kind: 5, AssetID: miner, SiteID: sites[i], ContainerID: miner, OrderID: "order", Start: historyStart, CapacityTHS: "100", SourceRef: fmt.Sprintf("coverage-%d", i)})
				reportEvidenceHours(t, f.db, f, allocation, start.Add(time.Hour), 1, 1, 100e12)
			}
			savedReportInputs(t, f, client.AccountID, f.host, input, 0)

			// Replace the publisher's org-wide role with explicit site-scoped roles.
			// This makes removing one assignment a real loss of effective permission,
			// not a site-specific change masked by an inherited org-wide role.
			q := sqlc.New(f.db)
			var orgAssignment, role int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT uor.id,uor.role_id FROM user_organization_role uor JOIN role r ON r.id=uor.role_id WHERE uor.user_id=$1 AND uor.organization_id=$2 AND uor.scope_type='org' AND uor.deleted_at IS NULL AND r.builtin_key='SUPER_ADMIN'`, publisher, f.org).Scan(&orgAssignment, &role))
			require.NoError(t, q.UnassignRole(t.Context(), orgAssignment))
			assignments := make([]int64, len(sites))
			assignSite := func(i int) int64 {
				t.Helper()
				assignment, err := q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: publisher, OrganizationID: f.org, RoleID: role, ScopeType: "site", ScopeID: sql.NullInt64{Int64: sites[i], Valid: true}})
				require.NoError(t, err)
				return assignment.ID
			}
			for i := range sites {
				assignments[i] = assignSite(i)
			}
			checkPermissions := func(secondAllowed bool) {
				t.Helper()
				effective, err := authz.LoadEffectiveTx(t.Context(), q, publisher, f.org)
				require.NoError(t, err)
				require.True(t, effective.HasAnyPermission())
				for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead} {
					require.True(t, effective.Has(permission, authz.ResourceContext{SiteID: &sites[0]}), permission)
					require.Equal(t, secondAllowed, effective.Has(permission, authz.ResourceContext{SiteID: &sites[1]}), permission)
				}
			}
			checkPermissions(true)

			period := reporting.Period{Kind: "monthly", Start: start, End: start.AddDate(0, 1, 0), AsOf: reportAsOf(t, f), Timezone: "UTC"}
			oldPeriod := period
			oldPeriod.Start, oldPeriod.End = start.AddDate(0, -1, 0), start
			oldDraft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: oldPeriod, InputRevision: 1, OwnerUserID: owner})
			require.NoError(t, err)
			old, err := f.svc.PublishReport(t.Context(), publisher, f.org, client.AccountID, oldDraft.ReportID, oldDraft.Revision, "reviewed incomplete sources")
			require.NoError(t, err, "the scoped publisher must be authorized before revocation")
			before := downloadReportArtifact(t, f, f.user, client.AccountID, old.ReportID, "evidence.zip")
			require.NotEmpty(t, before)
			draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: period, InputRevision: 1, OwnerUserID: owner})
			require.NoError(t, err)
			require.ElementsMatch(t, sites, draft.SiteIDs)
			_, err = f.svc.HostReport(t.Context(), publisher, f.org, client.AccountID, draft.ReportID)
			require.NoError(t, err, "the publisher can access the frozen draft before narrowing")
			frozen := func() string {
				t.Helper()
				return strings.Join(frozenReportEvidence(t, f, draft.ID), ",")
			}
			links := frozen()
			for i, allocation := range allocations {
				require.Contains(t, links, fmt.Sprintf("%d %s closed 1", allocation, start.Add(time.Hour).Format(time.RFC3339)), "the frozen draft must contain hourly evidence at site %d", sites[i])
			}
			var auditsBefore int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='publish_report'`, f.org).Scan(&auditsBefore))

			switch narrowing {
			case "reporting_site_grant":
				require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: publisherUUID, CanCreate: true, SiteIDs: []int64{sites[0]}}, 1))
				checkPermissions(true)
			case "host_site_role":
				require.NoError(t, q.UnassignRole(t.Context(), assignments[1]))
				checkPermissions(false)
			}
			grant, err := q.GetReportingAuthority(t.Context(), sqlc.GetReportingAuthorityParams{OrganizationID: f.org, AccountID: client.ID, UserID: publisher})
			require.NoError(t, err)
			require.True(t, grant.CanCreate, "this exercises the site boundary, not loss of all reporting authority")
			if narrowing == "reporting_site_grant" {
				require.False(t, grant.AllSites)
				require.ElementsMatch(t, []int64{sites[0]}, grant.SiteIds)
			} else {
				require.True(t, grant.AllSites, "the reporting grant must remain intact when host site permission is removed")
			}

			_, err = f.svc.PublishReport(t.Context(), publisher, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete sources")
			require.True(t, fleeterror.IsForbiddenError(err), "revoked site authority must deny publication, got %v", err)
			var status string
			var revision int64
			var issuedAt sql.NullTime
			var issuedBy sql.NullInt64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT status,revision,issued_at,issued_by FROM hosted_report WHERE id=$1`, draft.ID).Scan(&status, &revision, &issuedAt, &issuedBy))
			require.Equal(t, "draft", status)
			require.Equal(t, draft.Revision, revision)
			require.False(t, issuedAt.Valid)
			require.False(t, issuedBy.Valid)
			var artifacts, chunks, auditsAfter int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact WHERE report_internal_id=$1`, draft.ID).Scan(&artifacts))
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact_chunk c JOIN hosted_report_artifact a ON a.id=c.artifact_id WHERE a.report_internal_id=$1`, draft.ID).Scan(&chunks))
			require.Zero(t, artifacts)
			require.Zero(t, chunks)
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='publish_report'`, f.org).Scan(&auditsAfter))
			require.Equal(t, auditsBefore, auditsAfter)
			require.Equal(t, links, frozen(), "failed publication must not alter frozen links")
			_, err = f.svc.AccountReport(t.Context(), f.user, f.org, client.AccountID, draft.ReportID)
			require.Error(t, err, "the rejected draft is not client-visible")
			require.Equal(t, before, downloadReportArtifact(t, f, f.user, client.AccountID, old.ReportID, "evidence.zip"), "failed publication cannot mutate an older issued ZIP")

			// Restoring only the revoked authority should allow the same revision to
			// publish; this distinguishes denial from a broken publication fixture.
			switch narrowing {
			case "reporting_site_grant":
				require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: publisherUUID, CanCreate: true, AllSites: true}, 2))
			case "host_site_role":
				assignSite(1)
			}
			checkPermissions(true)
			issued, err := f.svc.PublishReport(t.Context(), publisher, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete sources")
			require.NoError(t, err)
			require.Equal(t, "issued", issued.Status)
			require.Equal(t, links, frozen())
			require.Equal(t, before, downloadReportArtifact(t, f, f.user, client.AccountID, old.ReportID, "evidence.zip"))
		})
	}
}
