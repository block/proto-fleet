package interfaces

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/sites/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=site.go -destination=mocks/mock_site_store.go -package=mocks SiteStore

// SiteStore is the persistence boundary for the sites domain. All
// methods are org-scoped; cross-org reads are not supported.
// CRUD, cascade-unassign, and the cross-collection helpers all live
// here so the delete cascade transaction stays single-store.
//
//nolint:interfacebloat // see comment above
type SiteStore interface {
	// CreateSite inserts a new site row and returns it. Maps a
	// unique-violation on (org_id, name) to AlreadyExists; maps a
	// slug unique-violation to models.ErrSiteSlugCollision so the
	// service can retry with the next suffix candidate.
	CreateSite(ctx context.Context, params models.CreateSiteParams) (*models.Site, error)

	// GetSite returns the live site or NotFound.
	GetSite(ctx context.Context, orgID, id int64) (*models.Site, error)

	// GetInfrastructureControlSubnets returns the site's canonical
	// newline-separated commissioned OT allowlist. The query is org-scoped and
	// excludes soft-deleted sites so cross-org/missing IDs are NotFound-masked.
	GetInfrastructureControlSubnets(ctx context.Context, orgID, siteID int64) (string, error)

	// SetInfrastructureControlSubnets explicitly replaces the site's canonical
	// commissioned OT allowlist. Empty text decommissions the site.
	SetInfrastructureControlSubnets(ctx context.Context, orgID, siteID int64, canonical string) (string, error)

	// GetSiteBySlug returns the live site with the given URL slug or
	// NotFound. The slug is not user-editable but is regenerated from the
	// name on a rename. Used by the route-scope resolver before checking
	// site-scoped permissions against the resolved site id.
	GetSiteBySlug(ctx context.Context, orgID int64, slug string) (*models.Site, error)

	// ListSites returns every live site in the org with attachment
	// counts, ordered by name.
	ListSites(ctx context.Context, orgID int64) ([]models.SiteWithCounts, error)

	// ListSiteSlugs returns every live site slug in the org. Used by
	// CreateSite to choose the first non-conflicting generated slug.
	ListSiteSlugs(ctx context.Context, orgID int64) ([]string, error)

	// CountRacksBySite returns the number of live racks attached to the
	// site in the org.
	CountRacksBySite(ctx context.Context, orgID, siteID int64) (int64, error)

	// CountBuildingsBySite returns the number of live buildings attached
	// to the site in the org.
	CountBuildingsBySite(ctx context.Context, orgID, siteID int64) (int64, error)

	// UpdateSite mutates the live site row. Maps unique-violation to
	// AlreadyExists; returns NotFound when the row is gone.
	UpdateSite(ctx context.Context, params models.UpdateSiteParams) (*models.Site, error)

	// SoftDeleteSite sets deleted_at; caller is responsible for the
	// surrounding transaction and cascading the rest of the impact
	// (devices, racks, buildings).
	SoftDeleteSite(ctx context.Context, orgID, id int64) (int64, error)

	// UnassignDevicesFromSite sets device.site_id = NULL for every
	// live device pointing at the site. Returns the count.
	UnassignDevicesFromSite(ctx context.Context, orgID, siteID int64) (int64, error)

	// DeleteCurtailmentResponseProfilesBySite deletes reusable
	// curtailment response behavior scoped to the site being deleted.
	DeleteCurtailmentResponseProfilesBySite(ctx context.Context, orgID, siteID int64) (int64, error)

	// LockInfrastructureDevicesBySiteForWrite locks live devices at the site
	// in ID order before a site-delete reference check and cascade.
	LockInfrastructureDevicesBySiteForWrite(ctx context.Context, orgID, siteID int64) ([]int64, error)

	// CountResponseProfilesByInfrastructureDevices counts surviving profiles
	// that reference any ID in the supplied set.
	CountResponseProfilesByInfrastructureDevices(ctx context.Context, orgID int64, ids []int64) (int64, error)

	// SoftDeleteBuildingsBySite soft-deletes every live building under
	// the site. Caller wraps it in the cascade tx.
	SoftDeleteBuildingsBySite(ctx context.Context, orgID, siteID int64) (int64, error)

	// SoftDeleteInfrastructureDevicesBySite soft-deletes every live
	// infrastructure device under the site so controllable facility
	// devices cannot outlive their site. Caller wraps it in the
	// cascade tx.
	SoftDeleteInfrastructureDevicesBySite(ctx context.Context, orgID, siteID int64) (int64, error)

	// UnassignRacksFromSite clears site_id on every rack directly
	// pointing at the site. Caller wraps it in the cascade tx.
	UnassignRacksFromSite(ctx context.Context, orgID, siteID int64) (int64, error)

	// UnassignRacksFromBuildingsBySite clears building_id (and the
	// free-form zone label) for every rack under any building of the
	// given site. Caller runs this BEFORE soft-deleting the buildings
	// so the join against building still resolves.
	UnassignRacksFromBuildingsBySite(ctx context.Context, orgID, siteID int64) (int64, error)

	// SiteBelongsToOrg returns true when a live site with the given
	// id exists in the org.
	SiteBelongsToOrg(ctx context.Context, orgID, id int64) (bool, error)

	// SitesByIDs returns the subset of the requested IDs that
	// correspond to live sites in the org. Caller diffs against the
	// requested set to detect cross-org or missing IDs. Mirrors
	// BuildingsByIDs; used to bulk-validate the rack-list site_ids
	// filter in one round trip.
	SitesByIDs(ctx context.Context, orgID int64, ids []int64) ([]int64, error)

	// LockSiteForWrite takes a row-lock on the site row for the
	// duration of the surrounding transaction, returning NotFound when
	// the site is missing or already soft-deleted. Callers that depend
	// on the target site being alive between the existence check and
	// a cascade write must use this instead of SiteBelongsToOrg to
	// avoid a TOCTOU vs concurrent DeleteSite.
	LockSiteForWrite(ctx context.Context, orgID, siteID int64) error

	// LockBuildingForWrite row-locks the building for the duration of the
	// surrounding transaction. Returns NotFound when the building is
	// already soft-deleted.
	LockBuildingForWrite(ctx context.Context, orgID, buildingID int64) error

	// LockBuildingsBySiteForWrite row-locks every live building under
	// the given site so DeleteSite's cascade serializes against any
	// concurrent AssignBuildingToSite touching one of those buildings.
	LockBuildingsBySiteForWrite(ctx context.Context, orgID, siteID int64) error

	// LockDevicesForReassign takes a row-lock on every matching live
	// device for the duration of the surrounding transaction so the
	// conflict check + UPDATE in AssignDevicesToSite are atomic
	// against a concurrent assign.
	LockDevicesForReassign(ctx context.Context, orgID int64, deviceIdentifiers []string) error

	// AssignDevicesToSite bulk-updates device.site_id for the
	// given identifiers. The caller must have validated cross-
	// collection conflicts (see FindDeviceSiteConflicts) and that
	// every identifier exists (see ListExistingDeviceIdentifiers).
	// targetSiteID == nil means "Unassigned".
	AssignDevicesToSite(ctx context.Context, orgID int64, targetSiteID *int64, deviceIdentifiers []string) (int64, error)

	// FindDeviceSiteConflicts returns, for each requested device that
	// is in a rack with a site_id, the device identifier and that
	// rack's site_id. The caller compares against the requested
	// target.
	FindDeviceSiteConflicts(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string]int64, error)

	// GetDistinctDeviceSiteIDs returns the distinct device.site_id values
	// (a nil entry for a site-less device) across the given identifiers,
	// for resolving the site scope of a multi-device activity event
	// (#538). Reduced via activity models.ResolveSiteScope.
	GetDistinctDeviceSiteIDs(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]*int64, error)

	// FindDevicesInSiteLessRacks returns the requested device
	// identifiers that sit in a live rack with NO site (a
	// fully-unassigned rack). The site peer of FindDeviceSiteConflicts:
	// such a device can't take a direct site while remaining in the
	// rack, so the caller flags it as a clearable conflict.
	FindDevicesInSiteLessRacks(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]string, error)

	// ListExistingDeviceIdentifiers returns the subset of the
	// requested identifiers that map to a live device in the org.
	ListExistingDeviceIdentifiers(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]string, error)

	// ListAllSiteNetworkConfigs returns every other live site's name
	// + raw network_config text in the org so the service can
	// compute cross-site overlap warnings on save. Excludes the row
	// being saved when excludeID > 0.
	ListAllSiteNetworkConfigs(ctx context.Context, orgID, excludeID int64) ([]models.SiteNetworkConfigEntry, error)

	// AssignBuildingToSite updates building.site_id for the given
	// building. targetSiteID == nil means "Unassigned". Returns the
	// number of rows affected (0 == not found).
	AssignBuildingToSite(ctx context.Context, orgID, buildingID int64, targetSiteID *int64) (int64, error)

	// AssignBuildingsToSiteBulk is the multi-building variant of
	// AssignBuildingToSite. Updates building.site_id for every building
	// in buildingIDs in one statement. Returns the row count actually
	// moved (skips soft-deleted rows).
	AssignBuildingsToSiteBulk(ctx context.Context, orgID int64, buildingIDs []int64, targetSiteID *int64) (int64, error)

	// ReassignRacksUnderBuilding cascades site_id down to every rack
	// under the given building. Caller wraps it in the same tx as the
	// building UPDATE.
	ReassignRacksUnderBuilding(ctx context.Context, orgID, buildingID int64, targetSiteID *int64) (int64, error)

	// ReassignRacksUnderBuildingsBulk cascades site_id down to every
	// rack under any building in buildingIDs, in one statement.
	ReassignRacksUnderBuildingsBulk(ctx context.Context, orgID int64, buildingIDs []int64, targetSiteID *int64) (int64, error)

	// ReassignDevicesUnderBuilding cascades site_id down to every
	// device in any rack of the given building. Caller wraps it in
	// the same tx as the building UPDATE.
	ReassignDevicesUnderBuilding(ctx context.Context, orgID, buildingID int64, targetSiteID *int64) (int64, error)

	// ReassignDevicesUnderBuildingsBulk cascades site_id down to every
	// device in any rack under any building in buildingIDs, in one
	// statement.
	ReassignDevicesUnderBuildingsBulk(ctx context.Context, orgID int64, buildingIDs []int64, targetSiteID *int64) (int64, error)
}
