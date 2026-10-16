package auth

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserInfoPermissions_OrgSharedNotes(t *testing.T) {
	siteID := int64(7)
	var fieldTechPermissions []string
	for _, role := range authz.BuiltinRoles() {
		if role.Key == authz.BuiltinKeyFieldTech {
			fieldTechPermissions = role.SeedPermissions
		}
	}
	require.NotEmpty(t, fieldTechPermissions)
	tests := []struct {
		name        string
		assignments []authz.Assignment
		want        []string
	}{
		{
			name: "site-only field technician can see and create notes",
			assignments: []authz.Assignment{{
				ScopeType:   authz.ScopeSite,
				SiteID:      &siteID,
				Permissions: fieldTechPermissions,
			}},
			want: []string{authz.PermNoteCreate, authz.PermNoteRead},
		},
		{
			name: "org grants retained and note grants deduplicated",
			assignments: []authz.Assignment{
				{ScopeType: authz.ScopeOrg, Permissions: []string{authz.PermFleetRead, authz.PermNoteRead}},
				{ScopeType: authz.ScopeSite, SiteID: &siteID, Permissions: []string{authz.PermNoteRead, authz.PermNoteManage, authz.PermSiteRead}},
			},
			want: []string{authz.PermFleetRead, authz.PermNoteManage, authz.PermNoteRead},
		},
		{
			name:        "unrelated site grants do not expose notes or default permissions",
			assignments: []authz.Assignment{{ScopeType: authz.ScopeSite, SiteID: &siteID, Permissions: []string{authz.PermSiteRead, authz.PermMinerBlinkLED}}},
			want:        []string{},
		},
		{
			name: "no grants expose no capabilities",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, userInfoPermissions(authz.NewEffectivePermissions(tt.assignments)))
		})
	}
}

func TestUserInfoPermissions_Nil(t *testing.T) {
	assert.Empty(t, userInfoPermissions(nil))
}
