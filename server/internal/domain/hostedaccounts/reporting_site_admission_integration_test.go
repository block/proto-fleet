package hostedaccounts_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func siteOnlyReportingInputs(t *testing.T, f fixture, account string, sites ...int64) reporting.Inputs {
	t.Helper()
	input := reportingInputs(t, f, account, f.host)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i, site := range sites {
		input.ExternalResults = append(input.ExternalResults, reporting.ContractResult{ID: fmt.Sprintf("result-%d", i), Metric: "availability", SiteID: site, Start: start, End: start.Add(time.Hour), Value: "99", Unit: "%", SourceRef: "source evidence", CalculationRef: "reviewed calculation"})
	}
	return input
}

func TestReportingSiteOnlyResultsRejectForeignAndMissingSites(t *testing.T) {
	for _, action := range []string{"save"} {
		for _, invalidSite := range []string{"foreign", "missing"} {
			t.Run(action+"/"+invalidSite, func(t *testing.T) {
				f := setup(t)
				client := f.client(t, "Reporting client", f.user, hostedaccounts.AllCapabilities)
				_, _ = reportingBoundaryActors(t, f, client.AccountID)
				var own, foreign, otherOrg int64
				require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Owned site','owned-site') RETURNING id`, f.org).Scan(&own))
				require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO organization(org_id,name) VALUES($1,'Other organization') RETURNING id`, uuid.NewString()).Scan(&otherOrg))
				require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Foreign site','foreign-site') RETURNING id`, otherOrg).Scan(&foreign))
				invalid := foreign
				if invalidSite == "missing" {
					require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT max(id)+1000000 FROM site`).Scan(&invalid))
				}
				input := siteOnlyReportingInputs(t, f, client.AccountID, own, invalid)
				require.NoError(t, input.Validate(), "site-only results satisfy the evidence format")
				var err error
				if action == "save" {
					_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "external results", input)
				}
				require.ErrorContains(t, err, "reporting site is outside the evidenced organization")
				var revisions int
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_reporting_inputs`).Scan(&revisions))
				if action == "save" {
					require.Zero(t, revisions, "invalid proposal leaves no revision")
				} else {
					require.Equal(t, 1, revisions, "invalid legacy draft remains unreviewed")
				}
			})
		}
	}
}

func TestReportingSiteOnlyResultsAllowRetainedOwnedSites(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprintf("soft_deleted_%t", deleted), func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Reporting client", f.user, hostedaccounts.AllCapabilities)
			_, _ = reportingBoundaryActors(t, f, client.AccountID)
			var site int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Historical site','historical-site') RETURNING id`, f.org).Scan(&site))
			if deleted {
				_, err := f.db.ExecContext(t.Context(), `UPDATE site SET deleted_at=clock_timestamp() WHERE id=$1`, site)
				require.NoError(t, err)
			}
			input := siteOnlyReportingInputs(t, f, client.AccountID, site, site)
			_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "retained site evidence", input)
			require.NoError(t, err)
		})
	}
}

func TestHostReportingDiscoveryRechecksRevokedHostPermissions(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Restricted reporting customer", f.user, hostedaccounts.AllCapabilities)
	actor, _ := reportingBoundaryActors(t, f, client.AccountID)
	accounts, err := f.svc.HostReportingAccounts(t.Context(), actor, f.org)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, client.AccountID, accounts[0].AccountID)
	var role int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='SUPER_ADMIN'`, f.org).Scan(&role))
	_, err = f.db.ExecContext(t.Context(), `UPDATE user_organization_role SET deleted_at=clock_timestamp() WHERE user_id=$1 AND organization_id=$2 AND deleted_at IS NULL`, actor, f.org)
	require.NoError(t, err)
	permissions, err := authz.LoadEffectiveTx(t.Context(), sqlc.New(f.db), actor, f.org)
	require.NoError(t, err)
	require.False(t, permissions.HasAnyPermission())
	grant, err := sqlc.New(f.db).GetReportingAuthority(t.Context(), sqlc.GetReportingAuthorityParams{OrganizationID: f.org, AccountID: client.ID, UserID: actor})
	require.NoError(t, err)
	require.True(t, grant.CanCreate, "reporting grant survives role revocation")
	accounts, err = f.svc.HostReportingAccounts(t.Context(), actor, f.org)
	require.ErrorContains(t, err, "account access denied")
	require.Empty(t, accounts, "discovery cannot return account metadata after host permission revocation")
	_, err = f.svc.HostReportingSetup(t.Context(), actor, f.org, client.AccountID)
	require.Error(t, err, "discovery and account admission enforce the same live permission prerequisite")
	_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: actor, OrganizationID: f.org, RoleID: role, ScopeType: "org"})
	require.NoError(t, err)
	accounts, err = f.svc.HostReportingAccounts(t.Context(), actor, f.org)
	require.NoError(t, err)
	require.Len(t, accounts, 1, "restored live host permission permits discovery again")
}
