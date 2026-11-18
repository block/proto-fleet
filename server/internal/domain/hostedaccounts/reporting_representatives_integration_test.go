package hostedaccounts_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestReportingRecipientsRequireLiveTeamManagement(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Reporting recipients", f.user, hostedaccounts.AllCapabilities)
	_, _ = reportActors(t, f, client.AccountID)
	var representative string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.user).Scan(&representative))
	input := reportingInputs(t, f, client.AccountID, f.host)
	input.Configuration.Representatives = []reporting.Representative{{UserID: representative, CanSubmit: true, SourceRef: "client authority"}}
	setup, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, client.AccountID)
	require.NoError(t, err)
	require.False(t, setup.CanManageRepresentatives, "organization reporting administration alone grants no client roster access")
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "recipient setup", input)
	require.ErrorContains(t, err, "team-management access")
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, client.AccountID, f.root, hostedaccounts.View|hostedaccounts.ManageMembers))
	setup, err = f.svc.HostReportingSetup(t.Context(), f.host, f.org, client.AccountID)
	require.NoError(t, err)
	require.True(t, setup.CanManageRepresentatives)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "recipient setup", input)
	require.NoError(t, err)
	require.NoError(t, f.svc.RevokeAncestorGrant(t.Context(), f.host, f.org, client.AccountID, f.root))
	input.Configuration.ContractRef = "other configuration change"
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 1, "unchanged recipients", input)
	require.NoError(t, err, "ordinary configuration edits preserve the existing recipients")
	input.Configuration.Representatives[0].CanSubmit = false
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 2, "authority change", input)
	require.ErrorContains(t, err, "team-management access")
	input.Configuration.Representatives = nil
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 2, "remove recipient", input)
	require.ErrorContains(t, err, "team-management access")
}
