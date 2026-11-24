package frigateupdate

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func ctxWith(assignments []authz.Assignment) context.Context {
	ctx := authn.SetInfo(context.Background(), &session.Info{UserID: 1, OrganizationID: 1, AuthMethod: session.AuthMethodSession, Username: "u"})
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions(assignments))
}

func TestAuthorizer(t *testing.T) {
	site7, site8 := int64(7), int64(8)
	perm := []string{authz.PermControllerSoftwareUpdate}
	tests := []struct {
		name                           string
		assignments                    []authz.Assignment
		site7, site8, anySite, orgWide bool
	}{
		{"org wide", []authz.Assignment{{ScopeType: authz.ScopeOrg, Permissions: perm}}, true, true, true, true},
		{"site 7 only", []authz.Assignment{{ScopeType: authz.ScopeSite, SiteID: &site7, Permissions: perm}}, true, false, true, false},
		{"other permission", []authz.Assignment{{ScopeType: authz.ScopeOrg, Permissions: []string{authz.PermMinerFirmwareUpdate}}}, false, false, false, false},
		{"nothing", nil, false, false, false, false},
	}
	a := NewAuthorizer()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ctxWith(tt.assignments)
			check := func(err error, want bool, what string) {
				if want {
					assert.NoError(t, err, what)
				} else {
					require.Error(t, err, what)
					assert.True(t, fleeterror.IsForbiddenError(err), "%s: %v", what, err)
				}
			}
			check(a.AuthorizeSite(ctx, &site7), tt.site7, "site 7")
			check(a.AuthorizeSite(ctx, &site8), tt.site8, "site 8")
			check(a.AuthorizeAnySite(ctx), tt.anySite, "any site")
			check(a.AuthorizeOrgWide(ctx), tt.orgWide, "org wide")
		})
	}
}

func TestAuthorizer_Unauthenticated(t *testing.T) {
	a := NewAuthorizer()
	ctx := context.Background()
	site := int64(1)
	for _, err := range []error{a.AuthorizeSite(ctx, &site), a.AuthorizeAnySite(ctx), a.AuthorizeOrgWide(ctx)} {
		require.Error(t, err)
		assert.True(t, fleeterror.IsAuthenticationError(err), err)
	}
}
