package diagnostics

import (
	"context"
	"log/slog"

	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	minerInterfaces "github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	minerModels "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	storeInterfaces "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
)

// PollResult contains the outcome of a PollErrors operation.
type PollResult struct {
	MinersProcessed int
	MinersFailed    int
	ErrorsUpserted  int
	UpsertsFailed   int
	Cancelled       bool
}

// Service manages diagnostic information polling and storage.
type Service struct {
	errorStore storeInterfaces.ErrorStore
}

// NewService creates a new diagnostics service.
func NewService(errorStore storeInterfaces.ErrorStore) *Service {
	return &Service{
		errorStore: errorStore,
	}
}

// GetError retrieves a single error by ID.
func (s *Service) GetError(ctx context.Context, orgID int64, errorID string) (*models.ErrorMessage, error) {
	return s.errorStore.GetErrorByErrorID(ctx, orgID, errorID)
}

// PollErrors fetches errors from each miner and upserts them to the datastore.
// Individual miner failures are logged and counted in PollResult. If the context
// is cancelled, processing stops and Cancelled is set to true in the result.
// Callers can check ctx.Err() to get the specific cancellation reason if needed.
func (s *Service) PollErrors(ctx context.Context, miners ...minerInterfaces.Miner) PollResult {
	var result PollResult

	for _, miner := range miners {
		select {
		case <-ctx.Done():
			result.Cancelled = true
			return result
		default:
		}

		deviceID := miner.GetID()
		orgID := miner.GetOrgID()

		deviceErrors, err := miner.GetErrors(ctx)
		if err != nil {
			slog.Warn("failed to get errors from miner", "deviceID", deviceID, "orgID", orgID, "error", err)
			result.MinersFailed++
			continue
		}

		result.MinersProcessed++

		if len(deviceErrors.Errors) == 0 {
			continue
		}

		upserted, failed := s.upsertErrors(ctx, orgID, deviceID, deviceErrors.Errors)
		result.ErrorsUpserted += upserted
		result.UpsertsFailed += failed
	}
	return result
}

// upsertErrors upserts a list of errors for a single device.
// Returns the count of successful upserts and failed upserts.
func (s *Service) upsertErrors(ctx context.Context, orgID int64, deviceID minerModels.DeviceIdentifier, errors []models.ErrorMessage) (upserted, failed int) {
	for i := range errors {
		_, err := s.errorStore.UpsertError(ctx, orgID, string(deviceID), &errors[i])
		if err != nil {
			slog.Warn("failed to upsert error", "deviceID", deviceID, "orgID", orgID, "minerError", errors[i].MinerError, "error", err)
			failed++
			continue
		}
		upserted++
	}
	return upserted, failed
}

// ListMinerErrors returns metadata for all canonical miner error codes.
func (s *Service) ListMinerErrors(_ context.Context) map[models.MinerError]models.MinerErrorInfo {
	return models.GetMinerErrorInfo()
}
