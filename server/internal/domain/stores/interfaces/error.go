package interfaces

import (
	"context"

	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
)

//go:generate mockgen -source=error.go -destination=mocks/mock_error_store.go -package=mocks ErrorStore

// ErrorStore defines the interface for error-related operations in the store layer.
type ErrorStore interface {
	// UpsertError inserts a new error or updates an existing open error with the same dedup key.
	// If an open error (closed_at IS NULL) exists with matching (org_id, device_id, miner_error,
	// component_id, component_type), it updates the mutable fields.
	// If no open error exists (or only closed), it inserts a new error with a new ULID.
	// Returns the full error record after the operation.
	UpsertError(ctx context.Context, orgID int64, deviceIdentifier string, errMsg *models.ErrorMessage) (*models.ErrorMessage, error)
}
