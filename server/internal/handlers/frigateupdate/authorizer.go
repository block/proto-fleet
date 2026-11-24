package frigateupdate

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/frigateupdate"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// authorizer enforces controller:software_update with the shared RBAC helpers.
type authorizer struct{}

var _ frigateupdate.Authorizer = authorizer{}

// NewAuthorizer returns the production Authorizer for the domain service.
func NewAuthorizer() frigateupdate.Authorizer { return authorizer{} }

func (authorizer) AuthorizeSite(ctx context.Context, siteID *int64) error {
	_, err := middleware.RequirePermission(ctx, authz.PermControllerSoftwareUpdate, authz.ResourceContext{SiteID: siteID})
	return err
}

func (authorizer) AuthorizeAnySite(ctx context.Context) error {
	_, err := middleware.RequirePermissionAtAnySite(ctx, authz.PermControllerSoftwareUpdate)
	return err
}

func (authorizer) AuthorizeOrgWide(ctx context.Context) error {
	_, err := middleware.RequireOrgWidePermission(ctx, authz.PermControllerSoftwareUpdate)
	return err
}
