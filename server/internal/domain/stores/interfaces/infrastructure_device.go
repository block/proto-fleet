package interfaces

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/infrastructure/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=infrastructure_device.go -destination=mocks/mock_infrastructure_device_store.go -package=mocks InfrastructureDeviceStore

// InfrastructureDeviceStore is the persistence boundary for the
// infrastructure domain. All methods are org-scoped.
type InfrastructureDeviceStore interface {
	// CreateInfrastructureDevice inserts a new device row. Maps a
	// unique-violation on (site_id, name) to AlreadyExists.
	CreateInfrastructureDevice(ctx context.Context, params models.CreateParams) (*models.Device, error)

	// GetInfrastructureDevice returns the live device or NotFound.
	GetInfrastructureDevice(ctx context.Context, orgID, id int64) (*models.Device, error)

	// ListInfrastructureDevices returns every live device in the org,
	// ordered by name. Filter optionally narrows to specific sites.
	ListInfrastructureDevices(ctx context.Context, filter models.ListFilter) ([]models.Device, error)

	// UpdateInfrastructureDevice mutates the row's mutable fields. The
	// write is predicated on params.ExpectedSiteID, so it returns
	// NotFound when the row is missing / soft-deleted / cross-org OR
	// has moved to a different site since authorization.
	UpdateInfrastructureDevice(ctx context.Context, params models.UpdateParams) (*models.Device, error)

	// SoftDeleteInfrastructureDevice sets deleted_at, predicated on
	// expectedSiteID. found is false when no live device matched
	// (missing / already-deleted / cross-org / moved sites since
	// authorization).
	SoftDeleteInfrastructureDevice(ctx context.Context, orgID, id, expectedSiteID int64) (found bool, err error)
}
