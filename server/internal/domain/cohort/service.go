// Package cohort is the domain layer for cohort CRUD.
package cohort

import (
	"context"
	"fmt"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Service is the domain entry point for cohort CRUD.
type Service struct {
	store   interfaces.CohortStore
	audit   AuditLogger
	metrics Metrics
}

// Option configures a Service.
type Option func(*Service)

// WithAuditLogger wires activity logging.
func WithAuditLogger(logger AuditLogger) Option {
	return func(s *Service) {
		if logger != nil {
			s.audit = logger
		}
	}
}

// WithMetrics wires operational metrics.
func WithMetrics(metrics Metrics) Option {
	return func(s *Service) {
		if metrics != nil {
			s.metrics = metrics
		}
	}
}

// NewService returns a cohort service.
func NewService(store interfaces.CohortStore, opts ...Option) *Service {
	s := &Service{
		store:   store,
		audit:   NoOpAuditLogger{},
		metrics: NoOpMetrics{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreateCohort validates and inserts a cohort plus explicit members.
func (s *Service) CreateCohort(ctx context.Context, params models.CreateCohortParams) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	params.Label = strings.TrimSpace(params.Label)
	params.Purpose = strings.TrimSpace(params.Purpose)
	if params.Label == "" {
		return nil, fleeterror.NewInvalidArgumentError("cohort label is required")
	}
	if params.Purpose == "" {
		return nil, fleeterror.NewInvalidArgumentError("cohort purpose is required")
	}
	if params.DesiredFirmwareFileID != nil && strings.TrimSpace(*params.DesiredFirmwareFileID) == "" {
		params.DesiredFirmwareFileID = nil
	}
	if params.SourceActorType == "" {
		params.SourceActorType = models.SourceActorUser
	}
	if err := validateUniqueDeviceIdentifiers(params.DeviceIdentifiers); err != nil {
		return nil, err
	}

	created, err := s.store.CreateCohort(ctx, params)
	if err != nil {
		return nil, err
	}
	s.auditCohortCreated(ctx, created)
	return created, nil
}

// GetCohort returns a cohort with explicit members.
func (s *Service) GetCohort(ctx context.Context, orgID, cohortID int64) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	return s.store.GetCohort(ctx, orgID, cohortID)
}

// ListCohorts returns cohorts for an org.
func (s *Service) ListCohorts(ctx context.Context, params models.ListCohortsParams) ([]*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	return s.store.ListCohorts(ctx, params)
}

// ListCohortsByOwner returns cohorts owned by a user.
func (s *Service) ListCohortsByOwner(ctx context.Context, params models.ListCohortsByOwnerParams) ([]*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	return s.store.ListCohortsByOwner(ctx, params)
}

// DeleteCohort soft-deletes a cohort by releasing it and clearing memberships.
func (s *Service) DeleteCohort(ctx context.Context, orgID, cohortID int64) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	cohort, err := s.store.ReleaseCohort(ctx, orgID, cohortID)
	if err != nil {
		return nil, err
	}
	s.auditCohortDeleted(ctx, cohort)
	return cohort, nil
}

func validateUniqueDeviceIdentifiers(ids []string) error {
	seen := make(map[string]struct{}, len(ids))
	for i, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return fleeterror.NewInvalidArgumentErrorf("device_identifiers[%d] is empty", i)
		}
		if _, ok := seen[id]; ok {
			return fleeterror.NewInvalidArgumentErrorf("duplicate device identifier %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (s *Service) auditCohortCreated(ctx context.Context, cohort *models.Cohort) {
	if cohort == nil {
		return
	}
	orgID := cohort.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           activityTypeCreated,
		OrganizationID: &orgID,
		Description:    fmt.Sprintf("Created cohort %q (id=%d)", cohort.Label, cohort.ID),
		Metadata: map[string]any{
			"cohort_id": cohort.ID,
			"label":     cohort.Label,
		},
	}
	activity.StampActor(ctx, &event)
	s.audit.Log(ctx, event)
}

func (s *Service) auditCohortDeleted(ctx context.Context, cohort *models.Cohort) {
	if cohort == nil {
		return
	}
	orgID := cohort.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           activityTypeDeleted,
		OrganizationID: &orgID,
		Description:    fmt.Sprintf("Deleted cohort %q (id=%d)", cohort.Label, cohort.ID),
		Metadata: map[string]any{
			"cohort_id": cohort.ID,
			"label":     cohort.Label,
		},
	}
	activity.StampActor(ctx, &event)
	s.audit.Log(ctx, event)
}
