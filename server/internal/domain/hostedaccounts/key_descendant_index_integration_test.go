package hostedaccounts_test

import (
	"encoding/json"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
)

func TestAccountKeyDescendantCleanupUsesAccountIndex(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.host, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.host, f.org, parent.AccountID, "Removed child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	sibling, err := f.svc.CreateChild(t.Context(), f.host, f.org, parent.AccountID, "Retained child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)

	// Populate 100 credentials with 100 children each, so one child's lookup
	// is selective within an organization. Keep triggers and planner defaults on.
	_, err = f.db.ExecContext(t.Context(), `
INSERT INTO hosted_account (account_id, organization_id, parent_id, depth, name, capabilities)
SELECT gen_random_uuid()::text, $1, $2, 2, 'Other child ' || n, 1
FROM generate_series(1, 98) n`, f.org, parent.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `
INSERT INTO api_key (key_id, name, prefix, key_hash, user_id, organization_id, subject_kind)
SELECT gen_random_uuid()::text, 'Index fixture', 'index' || lpad(n::text, 7, '0'),
       gen_random_uuid()::text, $1, $2, 'account'
FROM generate_series(1, 100) n`, f.host, f.org)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `
INSERT INTO hosted_account_key (api_key_id, organization_id, account_id, capabilities)
SELECT id, organization_id, $2, 1 FROM api_key
WHERE organization_id = $1 AND name = 'Index fixture'`, f.org, parent.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `
INSERT INTO hosted_account_key_descendant (organization_id, api_key_id, account_id)
SELECT k.organization_id, k.api_key_id, child.id
FROM hosted_account_key k JOIN hosted_account child
  ON child.organization_id = k.organization_id AND child.parent_id = k.account_id
WHERE k.organization_id = $1 AND k.account_id = $2`, f.org, parent.ID)
	require.NoError(t, err)

	other := f.client(t, "Other customer", f.host, hostedaccounts.AllCapabilities)
	otherChild, err := f.svc.CreateChild(t.Context(), f.host, f.org, other.AccountID, "Other customer's child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	credentials := apikey.NewService(sqlstores.NewSQLApiKeyStore(f.db), nil)
	keys := hostedaccounts.NewKeyService(f.svc, credentials)
	issued, err := keys.Create(t.Context(), f.host, f.org, other.AccountID, hostedaccounts.KeyOptions{
		Name: "Other customer's credential", Capabilities: hostedaccounts.View,
		DescendantAccountIDs: []string{otherChild.AccountID},
	})
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `ANALYZE hosted_account_key_descendant`)
	require.NoError(t, err)

	// This is the predicate used to find the referencing rows during an
	// account's FK cascade; no credential ID is available for the primary key.
	var encoded []byte
	require.NoError(t, f.db.QueryRowContext(t.Context(), `
EXPLAIN (ANALYZE, FORMAT JSON)
SELECT api_key_id FROM hosted_account_key_descendant
WHERE organization_id = $1 AND account_id = $2`, f.org, child.ID).Scan(&encoded))
	type planNode struct {
		IndexName string     `json:"Index Name"`
		Rows      float64    `json:"Actual Rows"`
		Plans     []planNode `json:"Plans"`
	}
	var plan []struct{ Plan planNode }
	require.NoError(t, json.Unmarshal(encoded, &plan))
	require.Len(t, plan, 1)
	require.Equal(t, float64(100), plan[0].Plan.Rows)
	var usesIndex func(planNode) bool
	usesIndex = func(node planNode) bool {
		if node.IndexName == "hosted_account_key_descendant_account" {
			return true
		}
		for _, child := range node.Plans {
			if usesIndex(child) {
				return true
			}
		}
		return false
	}
	require.True(t, usesIndex(plan[0].Plan), "account cleanup can find its rows without scanning the organization's other bindings")

	_, err = f.db.ExecContext(t.Context(), `DELETE FROM hosted_account WHERE organization_id = $1 AND id = $2`, f.org, child.ID)
	require.NoError(t, err)
	var removed, retained, total int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `
SELECT count(*) FILTER (WHERE account_id = $2), count(*) FILTER (WHERE account_id = $3), count(*)
FROM hosted_account_key_descendant WHERE organization_id = $1`, f.org, child.ID, sibling.ID).Scan(&removed, &retained, &total))
	require.Zero(t, removed)
	require.Equal(t, 100, retained, "every sibling binding survives across the other credentials")
	require.Equal(t, 9901, total, "only the removed child's 100 bindings cascade")
	listed, _, err := keys.List(t.Context(), f.host, f.org, other.AccountID, 0, "")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, issued.Key.KeyID, listed[0].KeyID)
	require.Equal(t, []string{otherChild.AccountID}, listed[0].DescendantAccountIds)
}
