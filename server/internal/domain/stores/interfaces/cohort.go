package interfaces

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=cohort.go -destination=mocks/mock_cohort_store.go -package=mocks CohortStore

// CohortStore is the persistence boundary for the cohorts domain.
//
//nolint:interfacebloat // Cohort membership, lifecycle, and ownership queries form one transactional domain boundary.
type CohortStore interface {
	CreateCohort(ctx context.Context, params models.CreateCohortParams) (*models.Cohort, error)
	GetCohort(ctx context.Context, orgID, cohortID int64) (*models.Cohort, error)
	ListCohortFirmwareVersionEvents(ctx context.Context, orgID, cohortID int64, startTime, endTime time.Time) ([]models.FirmwareVersionEvent, error)
	ListCohorts(ctx context.Context, params models.ListCohortsParams) (models.PagedCohorts, error)
	ListCohortsByOwner(ctx context.Context, params models.ListCohortsByOwnerParams) (models.PagedCohorts, error)
	UpdateCohort(ctx context.Context, params models.UpdateCohortParams) (*models.Cohort, error)
	UpdateDefaultCohortFirmware(ctx context.Context, params models.UpdateCohortParams) (*models.Cohort, error)
	UpdateDefaultCohortConfig(ctx context.Context, params models.UpdateCohortParams) (*models.Cohort, error)
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

//nolint:interfacebloat // Firmware lifecycle transitions form one persistence boundary.
type CohortFirmwareEnforcementStore interface {
	ListOrgsWithFirmwareTargets(ctx context.Context) ([]int64, error)
	ListFirmwareEnforcementCandidates(ctx context.Context, orgID int64) ([]models.FirmwareEnforcementCandidate, error)
	ClearMissingFirmwareTarget(ctx context.Context, orgID int64, firmwareFileID string) (int64, error)
	ClaimFirmwareDispatch(ctx context.Context, params models.ClaimFirmwareDispatchParams) (bool, error)
	MarkFirmwareDispatched(ctx context.Context, params models.MarkFirmwareDispatchedParams) (bool, error)
	MarkFirmwareConfirmed(ctx context.Context, params models.MarkFirmwareConfirmedParams) (bool, error)
	MarkFirmwareDrifted(ctx context.Context, params models.MarkFirmwareDriftedParams) (bool, error)
	MarkFirmwareDispatchFailure(ctx context.Context, params models.MarkFirmwareDispatchFailureParams) (bool, error)
	MarkFirmwareDispatchHeld(ctx context.Context, params models.MarkFirmwareDispatchHeldParams) (bool, error)
	IsCommandBatchFinished(ctx context.Context, batchUUID string) (bool, error)
	UpsertCohortReconcilerHeartbeat(ctx context.Context, lastTickAt time.Time, lastTickUUID uuid.UUID, durationMS *int32, activeDeviceCount int32) error
}

// CohortConfigEnforcementStore is the dimension-agnostic persistence boundary
// shared by ordinary cohort configuration adapters.
//
//nolint:interfacebloat // Generic config lifecycle transitions form one persistence boundary.
type CohortConfigEnforcementStore interface {
	ListOrgsWithDesiredConfig(ctx context.Context) ([]int64, error)
	ListConfigEnforcementCandidates(ctx context.Context, orgID int64, dimension models.CohortConfigDimension) ([]models.ConfigEnforcementCandidate, error)
	UpsertDeviceConfigState(ctx context.Context, params models.UpsertDeviceConfigStateParams) error
	UpsertConfigSupport(ctx context.Context, params models.ConfigEnforcementMutationParams) error
	ClaimConfigDispatch(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	MarkConfigDispatched(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	MarkConfigConfirmed(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	MarkConfigDrifted(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	MarkConfigDispatchFailure(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	MarkConfigDispatchHeld(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	IsCommandBatchFinished(ctx context.Context, batchUUID string) (bool, error)
}
