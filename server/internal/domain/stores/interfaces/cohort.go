package interfaces

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=cohort.go -destination=mocks/mock_cohort_store.go -package=mocks CohortStore

// CohortStore is the persistence boundary for the cohorts domain.
//
//nolint:interfacebloat // Cohort membership, lifecycle, and ownership queries form one transactional domain boundary.
type CohortStore interface {
	CreateCohort(ctx context.Context, params models.CreateCohortParams) (*models.Cohort, error)
	GetCohort(ctx context.Context, orgID, cohortID int64) (*models.Cohort, error)
	ListCohorts(ctx context.Context, params models.ListCohortsParams) (models.PagedCohorts, error)
	ListCohortsByOwner(ctx context.Context, params models.ListCohortsByOwnerParams) (models.PagedCohorts, error)
	UpdateCohort(ctx context.Context, params models.UpdateCohortParams) (*models.Cohort, error)
	UpdateDefaultCohortFirmware(ctx context.Context, params models.UpdateCohortParams) (*models.Cohort, error)
	SetCohortFirmwareTarget(ctx context.Context, params models.SetCohortFirmwareTargetParams) (*models.Cohort, error)
	MoveDevicesToCohort(ctx context.Context, params models.MembershipMutationParams) (*models.Cohort, error)
	RemoveDevicesAndGetCohort(ctx context.Context, params models.MembershipMutationParams) (*models.Cohort, error)
	ReleaseCohort(ctx context.Context, orgID, cohortID int64) (*models.Cohort, error)
	SweepExpiredCohorts(ctx context.Context) ([]*models.Cohort, error)

	InsertCohortMember(ctx context.Context, params models.InsertCohortMemberParams) error
	DeleteCohortMemberships(ctx context.Context, orgID, cohortID int64, deviceIdentifiers []string) (int64, error)
	ListCohortMembers(ctx context.Context, orgID, cohortID int64) ([]models.CohortMember, error)
	ResolveEffectiveCohortForDevice(ctx context.Context, orgID int64, deviceIdentifier string) (*models.Cohort, error)
	ListDefaultCohortDevices(ctx context.Context, orgID int64) ([]models.DefaultCohortDevice, error)
	ListCohortDeviceOwnership(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]models.CohortDeviceOwnership, error)
	ListActiveOwnedCohortMemberships(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]models.CohortDeviceOwnership, error)
	ListDevices(ctx context.Context, params models.ListDevicesParams) (models.PagedCohortDevices, error)
}
