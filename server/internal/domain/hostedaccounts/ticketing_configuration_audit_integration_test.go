package hostedaccounts_test

import (
	"encoding/json"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestTicketingConfigurationAuditMatchesCommittedPolicy(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.DefaultCapabilities)
	for _, enabled := range []bool{false, true} {
		var beforeRevision int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT revision FROM hosted_account WHERE id=$1", account.ID).Scan(&beforeRevision))
		require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DefaultCapabilities, false, &enabled))
		var revision int64
		var actual bool
		require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT revision,ticketing_enabled FROM hosted_account WHERE id=$1", account.ID).Scan(&revision, &actual))
		require.Equal(t, beforeRevision+1, revision, "one policy change has one committed revision")
		require.Equal(t, enabled, actual)
		var raw []byte
		require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT metadata FROM activity_log WHERE organization_id=$1 AND event_type='configure_hosted_account' AND metadata->>'account_id'=$2 ORDER BY id DESC LIMIT 1", f.org, account.AccountID).Scan(&raw))
		var metadata map[string]any
		require.NoError(t, json.Unmarshal(raw, &metadata))
		require.Equal(t, !enabled, metadata["previous_ticketing_enabled"])
		require.Equal(t, enabled, metadata["ticketing_enabled"])
		require.Equal(t, float64(revision), metadata["account_revision"])
	}
	disabled := false
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DefaultCapabilities, false, &disabled))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DefaultCapabilities, false))
	var enabled bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT ticketing_enabled FROM hosted_account WHERE id=$1", account.ID).Scan(&enabled))
	require.False(t, enabled, "omitting ticketing preserves the existing policy")
}
