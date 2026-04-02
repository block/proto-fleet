package interfaces

import (
	"context"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=error.go -destination=mocks/mock_error_store.go -package=mocks ErrorStore

// ErrorStore defines the interface for error-related operations in the store layer.
type ErrorStore interface {
	// UpsertError inserts a new error or updates an existing open error with the same dedup key.
	// If an open error (closed_at IS NULL) exists with matching (org_id, device_id, miner_error,
	// component_id, component_type), it updates the mutable fields.
	// If no open error exists (or only closed), it inserts a new error with a new ULID.
	// Returns the full error record after the operation.
	UpsertError(ctx context.Context, orgID int64, deviceIdentifier string, errMsg *models.ErrorMessage) (*models.ErrorMessage, error)

	// QueryErrors retrieves errors matching filter criteria using AND logic.
	// All provided filter criteria must match for an error to be returned.
	// Time range and include_closed filters are always applied.
	// Returns errors sorted by severity ASC (critical first), last_seen_at DESC, error_id DESC.
	// TODO: Add OR logic support where any filter criterion can match.
	QueryErrors(ctx context.Context, opts *models.QueryOptions) ([]models.ErrorMessage, error)

	// CountErrors returns the total count of errors matching filter criteria.
	// Uses AND logic: all provided filter criteria must match.
	// TODO: Add OR logic support controlled by opts.Filter.Logic.
	CountErrors(ctx context.Context, opts *models.QueryOptions) (int64, error)

	// GetErrorByErrorID retrieves a single error by its ULID, scoped to organization.
	// Returns fleeterror.NotFoundError if the error does not exist.
	GetErrorByErrorID(ctx context.Context, orgID int64, errorID string) (*models.ErrorMessage, error)

	// ============================================================================
	// Entity-Based Pagination Methods
	// ============================================================================

	// QueryDeviceKeys retrieves distinct device keys (ID + worst severity) with errors matching filter criteria.
	// Returns device keys sorted by worst severity (critical first), then by device_id.
	// Used for ResultViewDevice pagination where PageSize represents device count.
	// The WorstSeverity field is required for correct cursor-based keyset pagination.
	QueryDeviceKeys(ctx context.Context, opts *models.QueryOptions) ([]models.DeviceKey, error)

	// CountDevices returns the count of distinct devices with errors matching filter criteria.
	CountDevices(ctx context.Context, opts *models.QueryOptions) (int64, error)

	// QueryComponentKeys retrieves distinct component keys (device_id, component_id, worst severity).
	// Returns component keys sorted by worst severity, then by device_id, then by component_id.
	// Used for ResultViewComponent pagination where PageSize represents component count.
	// The WorstSeverity field is required for correct cursor-based keyset pagination.
	QueryComponentKeys(ctx context.Context, opts *models.QueryOptions) ([]models.ComponentKey, error)

	// CountComponents returns the count of distinct components with errors matching filter criteria.
	CountComponents(ctx context.Context, opts *models.QueryOptions) (int64, error)

	// ============================================================================
	// Error Lifecycle Management
	// ============================================================================

	// CloseStaleErrors closes all open errors where last_seen_at is older than the threshold.
	// This is a bulk operation that operates globally across all organizations.
	// Returns the number of errors closed.
	CloseStaleErrors(ctx context.Context, threshold time.Duration) (int64, error)
}
