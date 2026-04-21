package diagnostics

import (
	"context"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerInterfaces "github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	storeInterfaces "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
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
	config     Config
	errorStore storeInterfaces.ErrorStore
	transactor storeInterfaces.Transactor
}

// NewService creates a new diagnostics service and starts the error closer goroutine.
// The closer runs until the provided context is cancelled.
func NewService(ctx context.Context, config Config, errorStore storeInterfaces.ErrorStore, transactor storeInterfaces.Transactor) *Service {
	s := &Service{
		config:     config,
		errorStore: errorStore,
		transactor: transactor,
	}

	go s.runCloser(ctx)

	return s
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
// Applies default ComponentType based on MinerError if not already specified.
// Sets LastSeenAt to current time if plugin didn't provide it (zero value).
func (s *Service) upsertErrors(ctx context.Context, orgID int64, deviceID minerModels.DeviceIdentifier, errors []models.ErrorMessage) (upserted, failed int) {
	for i := range errors {
		if errors[i].ComponentType == models.ComponentTypeUnspecified {
			errors[i].ComponentType = models.DefaultComponentTypeForMinerError(errors[i].MinerError)
		}
		// If plugin didn't provide LastSeenAt (zero value), set it to current polling time.
		// This supports plugins that don't track observation time (e.g., Proto plugin).
		if errors[i].LastSeenAt.IsZero() {
			errors[i].LastSeenAt = time.Now()
		}
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

// ============================================================================
// Query Methods
// ============================================================================

// Query retrieves errors matching the specified filter criteria and returns them
// in the requested result view format (flat list, grouped by component, or grouped by device).
// Pagination semantics depend on ResultView:
//   - ResultViewError: PageSize is number of errors, cursor tracks last error
//   - ResultViewDevice: PageSize is number of devices, cursor tracks last device ID
//   - ResultViewComponent: PageSize is number of components, cursor tracks last (device_id, component_id)
func (s *Service) Query(ctx context.Context, opts *models.QueryOptions) (*models.QueryResult, error) {
	if opts == nil {
		opts = &models.QueryOptions{}
	}
	opts.PageSize = NormalizePageSize(opts.PageSize)

	switch opts.ResultView {
	case models.ResultViewDevice:
		return s.queryByDevice(ctx, opts)
	case models.ResultViewComponent:
		return s.queryByComponent(ctx, opts)
	case models.ResultViewError, models.ResultViewUnspecified:
		fallthrough
	default:
		return s.queryByError(ctx, opts)
	}
}

// validateErrorCursor validates an error-based cursor token.
// Returns an error with InvalidArgument status if the token is invalid.
func validateErrorCursor(pageToken string) error {
	_, err := DecodeCursor(pageToken)
	if err != nil {
		return fleeterror.NewInvalidArgumentError("invalid page token: " + err.Error())
	}
	return nil
}

// validateDeviceCursor validates a device-based cursor token.
// Returns an error with InvalidArgument status if the token is invalid.
func validateDeviceCursor(pageToken string) error {
	_, _, err := DecodeDeviceCursor(pageToken)
	if err != nil {
		return fleeterror.NewInvalidArgumentError("invalid page token: " + err.Error())
	}
	return nil
}

// validateComponentCursor validates a component-based cursor token.
// Returns an error with InvalidArgument status if the token is invalid.
func validateComponentCursor(pageToken string) error {
	_, _, _, _, err := DecodeComponentCursor(pageToken)
	if err != nil {
		return fleeterror.NewInvalidArgumentError("invalid page token: " + err.Error())
	}
	return nil
}

// queryByError implements error-based pagination (original behavior).
// PageSize represents number of errors. Cursor is (severity, last_seen_at, error_id).
func (s *Service) queryByError(ctx context.Context, opts *models.QueryOptions) (*models.QueryResult, error) {
	if err := validateErrorCursor(opts.PageToken); err != nil {
		return nil, err
	}

	var errors []models.ErrorMessage
	var totalCount int64

	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		var err error
		errors, err = s.errorStore.QueryErrors(txCtx, opts)
		if err != nil {
			return err
		}

		totalCount, err = s.errorStore.CountErrors(txCtx, opts)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &models.QueryResult{
		TotalCount:    totalCount,
		NextPageToken: BuildNextPageToken(errors, opts.PageSize),
		Errors:        errors,
	}, nil
}

// queryByDevice implements device-based pagination for ResultViewDevice.
// PageSize represents number of devices. Each device includes ALL its errors.
// Uses two-query approach: first gets paginated device IDs, then fetches all errors for those devices.
func (s *Service) queryByDevice(ctx context.Context, opts *models.QueryOptions) (*models.QueryResult, error) {
	if err := validateDeviceCursor(opts.PageToken); err != nil {
		return nil, err
	}

	var deviceKeys []models.DeviceKey
	var totalDevices int64
	var errors []models.ErrorMessage

	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		var err error
		// Step 1: Get paginated device keys (with severity for cursor)
		deviceKeys, err = s.errorStore.QueryDeviceKeys(txCtx, opts)
		if err != nil {
			return err
		}

		// Step 2: Get total device count
		totalDevices, err = s.errorStore.CountDevices(txCtx, opts)
		if err != nil {
			return err
		}

		if len(deviceKeys) == 0 {
			return nil
		}

		// Step 3: Get ALL errors for these specific devices
		deviceIdentifiers := extractDeviceIdentifiersFromDeviceKeys(deviceKeys)
		errorOpts := cloneOptsWithDeviceFilter(opts, deviceIdentifiers)
		errors, err = s.errorStore.QueryErrors(txCtx, errorOpts)
		return err
	})
	if err != nil {
		return nil, err
	}

	if len(deviceKeys) == 0 {
		return &models.QueryResult{
			TotalCount: totalDevices,
			DeviceErrs: []models.DeviceErrorGroup{},
		}, nil
	}

	// Step 4: Group by device and build result
	deviceKeyMap := buildDeviceKeyMap(deviceKeys)
	deviceGroups := GroupByDevice(errors, deviceKeyMap)

	return &models.QueryResult{
		TotalCount:    totalDevices,
		NextPageToken: BuildNextDevicePageToken(deviceKeys, opts.PageSize),
		DeviceErrs:    deviceGroups,
	}, nil
}

// queryByComponent implements component-based pagination for ResultViewComponent.
// PageSize represents number of components. Each component includes ALL its errors.
// Uses two-query approach: first gets paginated component keys, then fetches all errors for those components.
func (s *Service) queryByComponent(ctx context.Context, opts *models.QueryOptions) (*models.QueryResult, error) {
	if err := validateComponentCursor(opts.PageToken); err != nil {
		return nil, err
	}

	var componentKeys []models.ComponentKey
	var totalComponents int64
	var errors []models.ErrorMessage

	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		var err error
		// Step 1: Get paginated component keys
		componentKeys, err = s.errorStore.QueryComponentKeys(txCtx, opts)
		if err != nil {
			return err
		}

		// Step 2: Get total component count
		totalComponents, err = s.errorStore.CountComponents(txCtx, opts)
		if err != nil {
			return err
		}

		if len(componentKeys) == 0 {
			return nil
		}

		// Step 3: Get ALL errors for these specific components
		// We filter by the device identifiers that have components in our result set
		deviceIdentifiers := extractDeviceIdentifiersFromComponentKeys(componentKeys)
		errorOpts := cloneOptsWithDeviceFilter(opts, deviceIdentifiers)
		errors, err = s.errorStore.QueryErrors(txCtx, errorOpts)
		return err
	})
	if err != nil {
		return nil, err
	}

	if len(componentKeys) == 0 {
		return &models.QueryResult{
			TotalCount:    totalComponents,
			ComponentErrs: []models.ComponentErrors{},
		}, nil
	}

	// Step 4: Group by component and build result
	componentKeyMap := buildComponentKeyMap(componentKeys)
	componentGroups := GroupByComponent(errors, componentKeyMap)

	return &models.QueryResult{
		TotalCount:    totalComponents,
		NextPageToken: BuildNextComponentPageToken(componentKeys, opts.PageSize),
		ComponentErrs: componentGroups,
	}, nil
}

// cloneOptsWithDeviceFilter creates a copy of opts with device filter set to specific device identifiers.
// Removes pagination (uses MaxPageSize) since we want ALL errors for the specified devices.
func cloneOptsWithDeviceFilter(opts *models.QueryOptions, deviceIdentifiers []string) *models.QueryOptions {
	filter := &models.QueryFilter{
		DeviceIdentifiers: deviceIdentifiers,
	}
	if opts.Filter != nil {
		filter.DeviceTypes = opts.Filter.DeviceTypes
		filter.ComponentIDs = opts.Filter.ComponentIDs
		filter.ComponentTypes = opts.Filter.ComponentTypes
		filter.MinerErrors = opts.Filter.MinerErrors
		filter.Severities = opts.Filter.Severities
		filter.TimeFrom = opts.Filter.TimeFrom
		filter.TimeTo = opts.Filter.TimeTo
		filter.IncludeClosed = opts.Filter.IncludeClosed
		filter.Logic = opts.Filter.Logic
	}
	return &models.QueryOptions{
		OrgID:     opts.OrgID,
		Filter:    filter,
		PageSize:  MaxPageSize,
		PageToken: "", // No cursor - fetch all errors for these devices
	}
}

// extractDeviceIdentifiersFromDeviceKeys returns device identifiers from device keys.
func extractDeviceIdentifiersFromDeviceKeys(keys []models.DeviceKey) []string {
	identifiers := make([]string, len(keys))
	for i, key := range keys {
		identifiers[i] = key.DeviceIdentifier
	}
	return identifiers
}

// extractDeviceIdentifiersFromComponentKeys returns unique device identifiers from component keys.
func extractDeviceIdentifiersFromComponentKeys(keys []models.ComponentKey) []string {
	seen := make(map[string]bool)
	var identifiers []string
	for _, key := range keys {
		if !seen[key.DeviceIdentifier] {
			seen[key.DeviceIdentifier] = true
			identifiers = append(identifiers, key.DeviceIdentifier)
		}
	}
	return identifiers
}

// buildDeviceKeyMap creates a mapping from device_identifier (string) to DeviceKey.
// Used for GroupByDevice to look up numeric device_id from string identifier.
func buildDeviceKeyMap(keys []models.DeviceKey) map[string]models.DeviceKey {
	keyMap := make(map[string]models.DeviceKey, len(keys))
	for _, key := range keys {
		keyMap[key.DeviceIdentifier] = key
	}
	return keyMap
}

// buildComponentKeyMap creates a mapping from component key string to ComponentKey.
// The key format is "{deviceIdentifier}_{componentID}" or "{deviceIdentifier}_device".
// Used for GroupByComponent to look up numeric device_id from string identifier.
func buildComponentKeyMap(keys []models.ComponentKey) map[string]models.ComponentKey {
	keyMap := make(map[string]models.ComponentKey, len(keys))
	for _, key := range keys {
		keyMap[buildComponentKeyFromKey(key)] = key
	}
	return keyMap
}

// ============================================================================
// Watch Methods
// ============================================================================

// Watch creates a streaming watch for errors that match the given options.
// Returns a channel that receives updates at the configured poll interval.
// The channel is closed when the context is cancelled.
//
// Note: Watch is designed for real-time change monitoring, not historical data retrieval.
// Only errors that change after the watch starts will be reported.
//
// Event types:
//   - WatchKindOpened: Newly created errors (first_seen_at within current poll window)
//   - WatchKindUpdated: Existing errors that were updated (first_seen_at before poll window)
//   - WatchKindClosed: Errors that have been resolved (closed_at is now set)
func (s *Service) Watch(ctx context.Context, orgID int64, opts *WatchOptions) (<-chan *WatchUpdate, error) {
	w := newWatcher(s, orgID, opts, s.config)

	go w.run(ctx)

	return w.updateChan, nil
}
