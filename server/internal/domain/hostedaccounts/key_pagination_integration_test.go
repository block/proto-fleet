package hostedaccounts_test

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
)

func TestAccountKeyListDefaultPageIsBounded(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	keys := hostedaccounts.NewKeyService(f.svc, apikey.NewService(sqlstores.NewSQLApiKeyStore(f.db), nil))
	for range 30 {
		_, err := keys.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.KeyOptions{Name: "Read", Capabilities: hostedaccounts.View})
		require.NoError(t, err)
	}
	rows, next, err := keys.List(t.Context(), f.host, f.org, account.AccountID, 0, "")
	require.NoError(t, err)
	require.Len(t, rows, 25)
	require.NotEmpty(t, next)
}

func TestAccountKeyPaginationTraversesTiesAndRetainsPrivateMetadata(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.host, f.org, account.AccountID, "Child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RoleViewer))
	credentials := apikey.NewService(sqlstores.NewSQLApiKeyStore(f.db), nil)
	keys := hostedaccounts.NewKeyService(f.svc, credentials)
	expected := map[string]hostedaccounts.AccountKey{}
	ownCount := 0
	timestamp := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour)
	for i := range 107 {
		issuer := f.host
		options := hostedaccounts.KeyOptions{Name: "Automation", Capabilities: hostedaccounts.View}
		if i%2 == 0 {
			issuer = f.user
			ownCount++
		} else {
			options.DescendantAccountIDs = []string{child.AccountID}
		}
		issued, err := keys.Create(t.Context(), issuer, f.org, account.AccountID, options)
		require.NoError(t, err)
		expected[issued.Key.KeyID] = issued.Key
		createdAt := timestamp
		if i >= 70 {
			createdAt = timestamp.Add(-time.Hour)
		}
		_, err = f.db.ExecContext(t.Context(), `UPDATE api_key SET created_at=$1 WHERE key_id=$2`, createdAt, issued.Key.KeyID)
		require.NoError(t, err)
	}
	first, next, err := keys.List(t.Context(), f.host, f.org, account.AccountID, 0, "")
	require.NoError(t, err)
	require.Len(t, first, 25)
	require.NotEmpty(t, next)
	maxPage, maxNext, err := keys.List(t.Context(), f.host, f.org, account.AccountID, 100, "")
	require.NoError(t, err)
	require.Len(t, maxPage, 100)
	require.NotEmpty(t, maxNext)
	var all []hostedaccounts.AccountKey
	token := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 20)
		page, next, err := keys.List(t.Context(), f.host, f.org, account.AccountID, 13, token)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page), 13)
		all = append(all, page...)
		if next == "" {
			break
		}
		token = next
	}
	require.Len(t, all, 107)
	seen := map[string]bool{}
	for i, row := range all {
		require.False(t, seen[row.KeyID])
		seen[row.KeyID] = true
		require.Equal(t, expected[row.KeyID].DescendantAccountIds, row.DescendantAccountIds)
		if i > 0 {
			prev := all[i-1]
			require.True(t, prev.CreatedAt.After(row.CreatedAt) || (prev.CreatedAt.Equal(row.CreatedAt) && prev.KeyID < row.KeyID))
		}
	}
	own, ownNext, err := keys.List(t.Context(), f.user, f.org, account.AccountID, 100, "")
	require.NoError(t, err)
	require.Len(t, own, ownCount)
	require.Empty(t, ownNext)
	for _, row := range own {
		require.Empty(t, row.DescendantAccountIds)
	}
	// Revoke a viewer-issued key as the administrator on every run.
	revokedID := own[0].KeyID
	// Keep expiry and revocation distinct even when random UUID ordering
	// puts that viewer-issued key first in the combined listing.
	expiredID := all[0].KeyID
	if expiredID == revokedID {
		expiredID = all[1].KeyID
	}
	require.NotEqual(t, revokedID, expiredID)
	// Expired keys remain visible; revocation removes only the revoked entry.
	_, err = f.db.ExecContext(t.Context(), `UPDATE api_key SET expires_at=$1 WHERE key_id=$2`, timestamp, expiredID)
	require.NoError(t, err)
	require.NoError(t, keys.Revoke(t.Context(), f.host, f.org, account.AccountID, revokedID))
	page, _, err := keys.List(t.Context(), f.host, f.org, account.AccountID, 100, "")
	require.NoError(t, err)
	require.Equal(t, expiredID, page[0].KeyID)
	require.True(t, page[0].ExpiresAt.Valid)
	for _, row := range page {
		require.NotEqual(t, revokedID, row.KeyID)
	}
	// EXPLAIN the actual generated SQL, proving the descendant subplan runs
	// only on the materialized page, not the complete credential population.
	assertAccountKeyPagePlan(t, f.db, f.org, account.ID, f.host)
}

func TestAccountKeyPaginationRejectsCrossScopeAndRechecksAuthority(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	other := f.client(t, "Other", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, other.AccountID, f.user, hostedaccounts.RoleViewer))
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RoleAdministrator))
	keys := hostedaccounts.NewKeyService(f.svc, apikey.NewService(sqlstores.NewSQLApiKeyStore(f.db), nil))
	for range 3 {
		_, err := keys.Create(t.Context(), f.user, f.org, account.AccountID, hostedaccounts.KeyOptions{Name: "Read", Capabilities: hostedaccounts.View})
		require.NoError(t, err)
	}
	_, token, err := keys.List(t.Context(), f.user, f.org, account.AccountID, 1, "")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	_, _, err = keys.List(t.Context(), f.user, f.org, other.AccountID, 1, token)
	require.Error(t, err)
	_, _, err = keys.List(t.Context(), f.host, f.org, account.AccountID, 1, token)
	require.True(t, fleeterror.IsInvalidArgumentError(err), "another administrator cannot borrow the issuer's cursor")
	for _, invalid := range []string{"not base64!", strings.Repeat("a", 513), base64.RawURLEncoding.EncodeToString([]byte("1|invalid")), token + "="} {
		_, _, err := keys.List(t.Context(), f.user, f.org, account.AccountID, 1, invalid)
		require.True(t, fleeterror.IsInvalidArgumentError(err))
	}
	for _, invalid := range []int32{-1, 101} {
		_, _, err := keys.List(t.Context(), f.user, f.org, account.AccountID, invalid, "")
		require.True(t, fleeterror.IsInvalidArgumentError(err))
	}
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RoleViewer))
	_, _, err = keys.List(t.Context(), f.user, f.org, account.AccountID, 1, token)
	require.True(t, fleeterror.IsInvalidArgumentError(err), "a cursor captures own-versus-all listing visibility")
	_, ownToken, err := keys.List(t.Context(), f.user, f.org, account.AccountID, 1, "")
	require.NoError(t, err)
	require.NotEmpty(t, ownToken)
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, account.AccountID, f.user))
	_, _, err = keys.List(t.Context(), f.user, f.org, account.AccountID, 1, ownToken)
	require.True(t, fleeterror.IsForbiddenError(err), "continuation cannot preserve revoked access")
}

func assertAccountKeyPagePlan(t *testing.T, conn *sql.DB, orgID, accountID, actorID int64) {
	t.Helper()
	source, err := parser.ParseFile(token.NewFileSet(), "../../../generated/sqlc/hosted_account_key.sql.go", nil, 0)
	require.NoError(t, err)
	var query string
	ast.Inspect(source, func(node ast.Node) bool {
		value, ok := node.(*ast.ValueSpec)
		if !ok || len(value.Names) != 1 || value.Names[0].Name != "listHostedAccountKeys" {
			return true
		}
		literal, ok := value.Values[0].(*ast.BasicLit)
		require.True(t, ok)
		query, err = strconv.Unquote(literal.Value)
		require.NoError(t, err)
		return false
	})
	require.NotEmpty(t, query)
	var encoded []byte
	require.NoError(t, conn.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON) "+query, orgID, accountID, actorID, true, nil, "", 26).Scan(&encoded))
	var plan []struct{ Plan map[string]any }
	require.NoError(t, json.Unmarshal(encoded, &plan))
	require.Len(t, plan, 1)
	var pageRows, descendantLoops float64
	var visit func(map[string]any)
	visit = func(node map[string]any) {
		if node["Subplan Name"] == "CTE page" {
			pageRows, _ = node["Actual Rows"].(float64)
		}
		if node["Parent Relationship"] == "SubPlan" {
			descendantLoops, _ = node["Actual Loops"].(float64)
		}
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				childPlan, ok := child.(map[string]any)
				require.True(t, ok)
				visit(childPlan)
			}
		}
	}
	visit(plan[0].Plan)
	require.Equal(t, float64(26), pageRows, "the CTE materializes only the requested page plus continuation witness")
	require.Equal(t, pageRows, descendantLoops, "retained descendant scopes expand only after the page is bounded")
	t.Logf("materialized key rows=%.0f, descendant expansions=%.0f from107 credentials", pageRows, descendantLoops)
}
