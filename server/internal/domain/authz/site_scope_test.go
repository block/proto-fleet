package authz

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthorizedSiteScopeAllowsOrgAndSiteResources(t *testing.T) {
	t.Run("org-wide scope excludes narrowed sites but retains unassigned resources", func(t *testing.T) {
		scope := AuthorizedSiteScope{OrgWide: true, SiteIDs: []int64{7, 9}}
		assert.True(t, scope.Allows(nil))
		assert.True(t, scope.Allows(int64Pointer(8)))
		assert.False(t, scope.Allows(int64Pointer(7)))
	})

	t.Run("site allowlist excludes unassigned and other sites", func(t *testing.T) {
		scope := AuthorizedSiteScope{SiteIDs: []int64{7, 9}}
		assert.False(t, scope.Allows(nil))
		assert.True(t, scope.Allows(int64Pointer(9)))
		assert.False(t, scope.Allows(int64Pointer(8)))
	})

	t.Run("context round trip", func(t *testing.T) {
		want := AuthorizedSiteScope{OrgWide: true, SiteIDs: []int64{7}}
		got, ok := AuthorizedSiteScopeFromContext(WithAuthorizedSiteScope(t.Context(), want))
		require.True(t, ok)
		assert.Equal(t, want, got)
	})
}

func int64Pointer(value int64) *int64 { return &value }
