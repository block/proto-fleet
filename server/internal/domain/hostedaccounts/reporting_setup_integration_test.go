package hostedaccounts_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestReportingSetupRetainsHistoricalInputForHostAuthorizedRemoval(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Historical setup", f.user, hostedaccounts.AllCapabilities)
	reviewer, _ := reportingBoundaryActors(t, f, account.AccountID)
	var a, b int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'A','a') RETURNING id`, f.org).Scan(&a))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'B','b') RETURNING id`, f.org).Scan(&b))
	inputs := siteOnlyReportingInputs(t, f, account.AccountID, a, b)
	revision, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "historical sites", inputs)
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, account.AccountID, true, []int64{a}))
	got, err := f.svc.HostReportingSetup(t.Context(), reviewer, f.org, account.AccountID)
	require.NoError(t, err)
	require.NotNil(t, got.Input)
	require.Equal(t, revision.Revision, got.Input.Revision)
}
