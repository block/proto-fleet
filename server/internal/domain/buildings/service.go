// Package buildings is the domain layer for the BuildingService RPC
// surface. CRUD + cascade-unassign-on-delete; site assignment lives on
// SiteService.AssignBuildingToSite where the cross-collection
// invariant is enforced.
package buildings

import (
	"context"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Event type constants for buildings activity logs.
const (
	eventBuildingCreated = "building.created"
	eventBuildingUpdated = "building.updated"
	eventBuildingDeleted = "building.deleted"
)

// Service is the domain entry point for building CRUD.
type Service struct {
	store       interfaces.BuildingStore
	siteStore   interfaces.SiteStore
	transactor  interfaces.Transactor
	activitySvc *activity.Service
}

// NewService wires a BuildingStore, SiteStore (for site existence
// validation), Transactor (for the delete cascade), and the activity
// Service used for fire-and-forget audit logs. activitySvc may be nil
// in tests or environments where activity logging is disabled.
func NewService(
	store interfaces.BuildingStore,
	siteStore interfaces.SiteStore,
	transactor interfaces.Transactor,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		store:       store,
		siteStore:   siteStore,
		transactor:  transactor,
		activitySvc: activitySvc,
	}
}

// logActivity is the nil-safe fire-and-forget wrapper. Audit failures
// must not fail user RPCs, so we use Service.Log (not LogStrict).
func (s *Service) logActivity(ctx context.Context, event activitymodels.Event) {
	if s.activitySvc == nil {
		return
	}
	s.activitySvc.Log(ctx, event)
}

// stampActor populates the per-user fields on an activity event from
// session.Info when present.
func stampActor(ctx context.Context, e *activitymodels.Event) {
	info, err := session.GetInfo(ctx)
	if err != nil || info == nil {
		return
	}
	if e.UserID == nil && info.ExternalUserID != "" {
		uid := info.ExternalUserID
		e.UserID = &uid
	}
	if e.Username == nil && info.Username != "" {
		uname := info.Username
		e.Username = &uname
	}
	if e.OrganizationID == nil && info.OrganizationID != 0 {
		oid := info.OrganizationID
		e.OrganizationID = &oid
	}
}

// CreateBuilding inserts a new building. If site_id is set, validates
// the site exists in the org.
func (s *Service) CreateBuilding(ctx context.Context, params models.CreateParams) (*models.Building, error) {
	if !params.DefaultRackOrderIndex.Valid() {
		return nil, fleeterror.NewInvalidArgumentError("invalid default_rack_order_index")
	}

	var b *models.Building
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		// Lock the parent site row when specified so a concurrent
		// DeleteSite can't soft-delete it between the live-site check
		// and the building insert. LockSiteForWrite returns NotFound
		// when the site is missing/already soft-deleted, which we
		// surface directly.
		if params.SiteID != nil && *params.SiteID > 0 {
			if err := s.siteStore.LockSiteForWrite(txCtx, params.OrgID, *params.SiteID); err != nil {
				return err
			}
		}
		created, err := s.store.CreateBuilding(txCtx, params)
		if err != nil {
			return err
		}
		b = created
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Activity log fires AFTER tx commits — RunInTx may retry the closure
	// on serialization failures, so an in-closure Log would duplicate.
	orgID := params.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventBuildingCreated,
		OrganizationID: &orgID,
		SiteID:         b.SiteID,
		Description:    fmt.Sprintf("Created building %q (id=%d)", b.Name, b.ID),
		Metadata: map[string]any{
			"building_id":   b.ID,
			"building_name": b.Name,
			"site_id":       b.SiteID,
		},
	}
	stampActor(ctx, &event)
	s.logActivity(ctx, event)

	return b, nil
}

// GetBuilding returns the live building or NotFound.
func (s *Service) GetBuilding(ctx context.Context, orgID, id int64) (*models.Building, error) {
	return s.store.GetBuilding(ctx, orgID, id)
}

// ListBuildings returns the filtered building list with rack counts.
func (s *Service) ListBuildings(ctx context.Context, filter models.ListFilter) ([]models.BuildingWithCounts, error) {
	// The proto oneof enforces mutual exclusion structurally; this is
	// a defense-in-depth guard for any non-proto caller.
	if filter.SiteID != nil && *filter.SiteID > 0 && filter.UnassignedOnly {
		return nil, fleeterror.NewInvalidArgumentError("site_id and unassigned_only are mutually exclusive")
	}
	return s.store.ListBuildings(ctx, filter)
}

// UpdateBuilding mutates the building's mutable fields. Site
// assignment is intentionally not handled here.
func (s *Service) UpdateBuilding(ctx context.Context, params models.UpdateParams) (*models.Building, error) {
	if !params.DefaultRackOrderIndex.Valid() {
		return nil, fleeterror.NewInvalidArgumentError("invalid default_rack_order_index")
	}
	b, err := s.store.UpdateBuilding(ctx, params)
	if err != nil {
		return nil, err
	}

	orgID := params.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventBuildingUpdated,
		OrganizationID: &orgID,
		SiteID:         b.SiteID,
		Description:    fmt.Sprintf("Updated building %q (id=%d)", b.Name, b.ID),
		Metadata: map[string]any{
			"building_id":   b.ID,
			"building_name": b.Name,
		},
	}
	stampActor(ctx, &event)
	s.logActivity(ctx, event)

	return b, nil
}

// DeleteBuilding soft-deletes the building and cascade-unassigns its
// racks in one transaction. Returns the impact count.
func (s *Service) DeleteBuilding(ctx context.Context, orgID, id int64) (*models.DeleteResult, error) {
	var out models.DeleteResult
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		rowsAffected, err := s.store.SoftDeleteBuilding(txCtx, orgID, id)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return fleeterror.NewNotFoundErrorf("building %d not found", id)
		}
		rackCount, err := s.store.UnassignRacksFromBuilding(txCtx, orgID, id)
		if err != nil {
			return err
		}
		out.UnassignedRackCount = rackCount
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Fire AFTER tx commits; RunInTx may retry the closure.
	orgIDVal := orgID
	buildingIDVal := id
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventBuildingDeleted,
		OrganizationID: &orgIDVal,
		Description: fmt.Sprintf(
			"Deleted building %d (%d racks unassigned)",
			buildingIDVal, out.UnassignedRackCount,
		),
		Metadata: map[string]any{
			"building_id":           buildingIDVal,
			"unassigned_rack_count": out.UnassignedRackCount,
		},
	}
	stampActor(ctx, &event)
	s.logActivity(ctx, event)

	return &out, nil
}
