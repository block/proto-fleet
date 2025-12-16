package errorquery

import (
	"context"
	"fmt"
	"sort"
	"time"

	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/session"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service provides error querying capabilities.
type Service struct {
	fakeManager *FakeErrorManager
	deviceStore interfaces.DeviceStore
}

// NewService creates a new error query service.
func NewService(fakeManager *FakeErrorManager, deviceStore interfaces.DeviceStore) *Service {
	return &Service{
		fakeManager: fakeManager,
		deviceStore: deviceStore,
	}
}

// Query retrieves errors with filtering and pagination.
func (s *Service) Query(ctx context.Context, req *errorsv1.QueryRequest) (*errorsv1.QueryResponse, error) {
	// Get org ID from context.
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get session info: %w", err)
	}
	orgID := info.OrganizationID

	// Decode page token.
	pageSize := NormalizePageSize(req.GetPageSize())
	cursor, err := DecodePageToken(req.GetPageToken())
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid page token: " + err.Error())
	}

	// Get device IDs to query.
	deviceIDs, deviceTypes, err := s.resolveDevices(ctx, orgID, req.GetFilter())
	if err != nil {
		return nil, fmt.Errorf("failed to resolve devices: %w", err)
	}

	// Clear generated errors to allow fresh generation (for testing)
	// This ensures we get a good distribution of errors across devices
	s.fakeManager.ClearGeneratedErrors()

	// Ensure errors exist for all devices.
	for i, deviceID := range deviceIDs {
		s.fakeManager.EnsureErrorsExist(deviceID, deviceTypes[i])
	}

	// Collect all errors for the devices.
	var allErrors []ErrorRecord
	for _, deviceID := range deviceIDs {
		errors := s.fakeManager.GetErrorsForDevice(deviceID)
		allErrors = append(allErrors, errors...)
	}

	// Apply filters.
	filteredErrors := s.applyFilters(allErrors, req.GetFilter(), deviceTypes, deviceIDs)

	// Sort errors.
	SortErrors(filteredErrors)

	// Build response based on result view.
	resultView := req.GetResultView()
	if resultView == errorsv1.ResultView_RESULT_VIEW_UNSPECIFIED {
		resultView = errorsv1.ResultView_RESULT_VIEW_ERROR
	}

	switch resultView {
	case errorsv1.ResultView_RESULT_VIEW_COMPONENT:
		return s.buildComponentPageResponse(filteredErrors, deviceTypes, deviceIDs, cursor.Offset, pageSize, resultView)
	case errorsv1.ResultView_RESULT_VIEW_DEVICE:
		return s.buildDevicePageResponse(filteredErrors, deviceTypes, deviceIDs, cursor.Offset, pageSize, resultView)
	case errorsv1.ResultView_RESULT_VIEW_ERROR, errorsv1.ResultView_RESULT_VIEW_UNSPECIFIED:
		return s.buildErrorPageResponse(filteredErrors, cursor.Offset, pageSize, resultView)
	}
	// Unreachable due to exhaustive switch, but return default for safety.
	return s.buildErrorPageResponse(filteredErrors, cursor.Offset, pageSize, resultView)
}

// GetError retrieves a single error by ID.
func (s *Service) GetError(ctx context.Context, errorID string) (*errorsv1.ErrorMessage, error) {
	// Get org ID from context.
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get session info: %w", err)
	}
	orgID := info.OrganizationID

	// Look up the error.
	errorRecord, ok := s.fakeManager.GetErrorByID(errorID)
	if !ok {
		return nil, fleeterror.NewNotFoundError("error not found: " + errorID)
	}

	// Verify the device belongs to this org.
	if err := s.verifyDeviceOwnership(ctx, errorRecord.DeviceID, orgID); err != nil {
		return nil, fleeterror.NewNotFoundError("error not found: " + errorID)
	}

	return convertErrorRecordToProto(errorRecord), nil
}

// ListMinerErrors returns metadata for all miner error codes.
func (s *Service) ListMinerErrors(_ context.Context) (*errorsv1.ListMinerErrorsResponse, error) {
	metadata := s.fakeManager.GetMetadata()

	var items []*errorsv1.MinerErrorInfo
	for code, meta := range metadata {
		if code == errorsv1.MinerError_MINER_ERROR_UNSPECIFIED {
			continue
		}
		items = append(items, &errorsv1.MinerErrorInfo{
			Code:            meta.Code,
			Name:            meta.Name,
			DefaultSummary:  meta.DefaultSummary,
			DefaultSeverity: meta.DefaultSeverity,
			DefaultAction:   meta.DefaultAction,
			DefaultImpact:   meta.DefaultImpact,
		})
	}

	// Sort by code value.
	sort.Slice(items, func(i, j int) bool {
		return items[i].Code < items[j].Code
	})

	return &errorsv1.ListMinerErrorsResponse{Items: items}, nil
}

// Watch streams error updates (simulated for fake manager).
func (s *Service) Watch(ctx context.Context, filter *errorsv1.Filter) (<-chan *errorsv1.WatchResponse, error) {
	// Get org ID from context.
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get session info: %w", err)
	}
	orgID := info.OrganizationID

	// Get initial device list.
	deviceIDs, deviceTypes, err := s.resolveDevices(ctx, orgID, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve devices: %w", err)
	}

	// Ensure errors exist for all devices.
	for i, deviceID := range deviceIDs {
		s.fakeManager.EnsureErrorsExist(deviceID, deviceTypes[i])
	}

	ch := make(chan *errorsv1.WatchResponse, 10)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		// Send initial state.
		s.sendWatchUpdate(ch, deviceIDs, deviceTypes, filter, errorsv1.WatchResponse_KIND_OPENED)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Send periodic updates (simulating potential changes).
				s.sendWatchUpdate(ch, deviceIDs, deviceTypes, filter, errorsv1.WatchResponse_KIND_UPDATED)
			}
		}
	}()

	return ch, nil
}

// resolveDevices returns device IDs and their types for the given filter.
func (s *Service) resolveDevices(ctx context.Context, orgID int64, filter *errorsv1.Filter) ([]string, []string, error) {
	var deviceIDs []string
	var deviceTypes []string

	simpleFilter := filter.GetSimple()
	if simpleFilter != nil && len(simpleFilter.GetDeviceIdentifiers()) > 0 {
		// Use device IDs from filter.
		for _, id := range simpleFilter.GetDeviceIdentifiers() {
			// Verify each device belongs to org.
			if err := s.verifyDeviceOwnership(ctx, id, orgID); err != nil {
				continue // Skip devices that don't belong to this org.
			}
			deviceIDs = append(deviceIDs, id)
			// Get device type if stored.
			deviceTypes = append(deviceTypes, s.fakeManager.GetDeviceType(id))
		}
	} else {
		// Get all paired devices for org with their database IDs.
		// Use ListMinerStateSnapshots which returns device_id and model.
		devices, _, _, err := s.deviceStore.ListMinerStateSnapshots(ctx, orgID, "", 10000, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list devices: %w", err)
		}

		for _, device := range devices {
			if device.DeviceID == 0 {
				continue // Skip unpaired devices
			}
			deviceIDs = append(deviceIDs, device.DeviceIdentifier)
			deviceType := ""
			if device.Model.Valid {
				deviceType = device.Model.String
			}
			deviceTypes = append(deviceTypes, deviceType)

			// Store the device type mapping.
			s.fakeManager.SetDeviceType(device.DeviceIdentifier, deviceType)
		}
	}

	return deviceIDs, deviceTypes, nil
}

// verifyDeviceOwnership checks if a device belongs to the given org.
func (s *Service) verifyDeviceOwnership(_ context.Context, _ string, _ int64) error {
	// For the fake manager, we'll assume all devices are accessible.
	// In a real implementation, this would check the device store.
	return nil
}

// applyFilters filters errors based on the request filter.
func (s *Service) applyFilters(errors []ErrorRecord, filter *errorsv1.Filter, deviceTypes []string, deviceIDs []string) []ErrorRecord {
	if filter == nil {
		return errors
	}

	// Build device type map.
	deviceTypeMap := make(map[string]string)
	for i, id := range deviceIDs {
		if i < len(deviceTypes) {
			deviceTypeMap[id] = deviceTypes[i]
		}
	}

	var result []ErrorRecord
	for _, err := range errors {
		if s.matchesFilter(err, filter, deviceTypeMap) {
			result = append(result, err)
		}
	}
	return result
}

// matchesFilter checks if an error matches all filter criteria (AND logic).
// TODO(DASH-1048): Add OR logic support controlled by filter.SimpleLogic field.
func (s *Service) matchesFilter(err ErrorRecord, filter *errorsv1.Filter, deviceTypeMap map[string]string) bool {
	// Check include_closed.
	if !filter.GetIncludeClosed() && err.ClosedAt != nil {
		return false
	}

	// Check time range.
	if filter.GetTimeFrom() != nil && err.LastSeenAt.Before(filter.GetTimeFrom().AsTime()) {
		return false
	}
	if filter.GetTimeTo() != nil && err.LastSeenAt.After(filter.GetTimeTo().AsTime()) {
		return false
	}

	simpleFilter := filter.GetSimple()
	if simpleFilter == nil {
		return true
	}

	// AND logic: all provided filter criteria must match.

	// Check device IDs.
	if len(simpleFilter.GetDeviceIdentifiers()) > 0 {
		match := false
		for _, id := range simpleFilter.GetDeviceIdentifiers() {
			if err.DeviceID == id {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// Check device types.
	if len(simpleFilter.GetDeviceTypes()) > 0 {
		match := false
		deviceType := deviceTypeMap[err.DeviceID]
		for _, dt := range simpleFilter.GetDeviceTypes() {
			if deviceType == dt {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// Check component IDs.
	if len(simpleFilter.GetComponentIds()) > 0 {
		match := false
		for _, cid := range simpleFilter.GetComponentIds() {
			if err.ComponentID == cid {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// Check component types.
	if len(simpleFilter.GetComponentTypes()) > 0 {
		match := false
		errCompType := GetComponentTypeForError(err.MinerError)
		for _, ct := range simpleFilter.GetComponentTypes() {
			if errCompType == ct {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// Check miner errors.
	if len(simpleFilter.GetCanonicalErrors()) > 0 {
		match := false
		for _, ce := range simpleFilter.GetCanonicalErrors() {
			if err.MinerError == ce {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// Check severities.
	if len(simpleFilter.GetSeverities()) > 0 {
		match := false
		for _, sev := range simpleFilter.GetSeverities() {
			if err.Severity == sev {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	return true
}

// buildErrorPageResponse creates a response with a flat list of errors.
func (s *Service) buildErrorPageResponse(errors []ErrorRecord, offset, pageSize int, view errorsv1.ResultView) (*errorsv1.QueryResponse, error) {
	page, nextToken, totalCount := Paginate(errors, offset, pageSize, view)

	items := make([]*errorsv1.ErrorMessage, len(page))
	for i, err := range page {
		items[i] = convertErrorRecordToProto(&err)
	}

	return &errorsv1.QueryResponse{
		Result: &errorsv1.QueryResponse_Errors{
			Errors: &errorsv1.Errors{Items: items},
		},
		NextPageToken: nextToken,
		TotalCount:    totalCount,
	}, nil
}

// buildComponentPageResponse creates a response grouped by component.
func (s *Service) buildComponentPageResponse(errors []ErrorRecord, deviceTypes []string, deviceIDs []string, offset, pageSize int, view errorsv1.ResultView) (*errorsv1.QueryResponse, error) {
	// Group errors by component.
	componentMap := make(map[string][]ErrorRecord)
	for _, err := range errors {
		key := err.ComponentID
		if key == "" {
			key = fmt.Sprintf("%s_device", err.DeviceID)
		}
		componentMap[key] = append(componentMap[key], err)
	}

	// Build device type map.
	deviceTypeMap := make(map[string]string)
	for i, id := range deviceIDs {
		if i < len(deviceTypes) {
			deviceTypeMap[id] = deviceTypes[i]
		}
	}

	// Convert to component errors.
	var componentErrors []*errorsv1.ComponentError
	for componentID, compErrors := range componentMap {
		if len(compErrors) == 0 {
			continue
		}

		// Parse component ID to get type.
		_, compType, _, ok := ParseComponentID(componentID)
		if !ok {
			// If parsing fails, fall back to getting component type from the first error's metadata
			if len(compErrors) > 0 {
				compType = GetComponentTypeForError(compErrors[0].MinerError)
			}
			// If still unspecified, try to determine from all errors in this component
			if compType == errorsv1.ComponentType_COMPONENT_TYPE_UNSPECIFIED {
				for _, err := range compErrors {
					ct := GetComponentTypeForError(err.MinerError)
					if ct != errorsv1.ComponentType_COMPONENT_TYPE_UNSPECIFIED {
						compType = ct
						break
					}
				}
			}
		}

		deviceID := compErrors[0].DeviceID

		items := make([]*errorsv1.ErrorMessage, len(compErrors))
		for i, err := range compErrors {
			items[i] = convertErrorRecordToProto(&err)
		}

		componentErrors = append(componentErrors, &errorsv1.ComponentError{
			ComponentId:      componentID,
			ComponentType:    compType,
			DeviceIdentifier: deviceID,
			Status:           CalculateStatus(compErrors),
			Summary:          GenerateSummary(compErrors),
			Errors:           items,
			CountsBySeverity: CountsBySeverity(compErrors),
		})
	}

	// Sort by status (ERROR first).
	sort.Slice(componentErrors, func(i, j int) bool {
		return componentErrors[i].Status > componentErrors[j].Status
	})

	page, nextToken, totalCount := Paginate(componentErrors, offset, pageSize, view)

	return &errorsv1.QueryResponse{
		Result: &errorsv1.QueryResponse_Components{
			Components: &errorsv1.ComponentErrors{Items: page},
		},
		NextPageToken: nextToken,
		TotalCount:    totalCount,
	}, nil
}

// buildDevicePageResponse creates a response grouped by device.
func (s *Service) buildDevicePageResponse(errors []ErrorRecord, deviceTypes []string, deviceIDs []string, offset, pageSize int, view errorsv1.ResultView) (*errorsv1.QueryResponse, error) {
	// Group errors by device.
	deviceMap := make(map[string][]ErrorRecord)
	for _, err := range errors {
		deviceMap[err.DeviceID] = append(deviceMap[err.DeviceID], err)
	}

	// Build device type map.
	deviceTypeMap := make(map[string]string)
	for i, id := range deviceIDs {
		if i < len(deviceTypes) {
			deviceTypeMap[id] = deviceTypes[i]
		}
	}

	// Convert to device errors.
	var deviceErrors []*errorsv1.DeviceError
	for deviceID, devErrors := range deviceMap {
		items := make([]*errorsv1.ErrorMessage, len(devErrors))
		for i, err := range devErrors {
			items[i] = convertErrorRecordToProto(&err)
		}

		deviceErrors = append(deviceErrors, &errorsv1.DeviceError{
			DeviceIdentifier: deviceID,
			DeviceType:       deviceTypeMap[deviceID],
			Status:           CalculateStatus(devErrors),
			Summary:          GenerateSummary(devErrors),
			Errors:           items,
			CountsBySeverity: CountsBySeverity(devErrors),
		})
	}

	// Sort by status (ERROR first).
	sort.Slice(deviceErrors, func(i, j int) bool {
		return deviceErrors[i].Status > deviceErrors[j].Status
	})

	page, nextToken, totalCount := Paginate(deviceErrors, offset, pageSize, view)

	return &errorsv1.QueryResponse{
		Result: &errorsv1.QueryResponse_Devices{
			Devices: &errorsv1.DeviceErrors{Items: page},
		},
		NextPageToken: nextToken,
		TotalCount:    totalCount,
	}, nil
}

// sendWatchUpdate sends an update event on the watch channel.
func (s *Service) sendWatchUpdate(ch chan<- *errorsv1.WatchResponse, deviceIDs []string, deviceTypes []string, filter *errorsv1.Filter, kind errorsv1.WatchResponse_Kind) {
	// Collect all errors.
	var allErrors []ErrorRecord
	for _, deviceID := range deviceIDs {
		errors := s.fakeManager.GetErrorsForDevice(deviceID)
		allErrors = append(allErrors, errors...)
	}

	// Apply filters.
	filteredErrors := s.applyFilters(allErrors, filter, deviceTypes, deviceIDs)

	// Sort.
	SortErrors(filteredErrors)

	// Convert to proto.
	items := make([]*errorsv1.ErrorMessage, len(filteredErrors))
	for i, err := range filteredErrors {
		items[i] = convertErrorRecordToProto(&err)
	}

	response := &errorsv1.WatchResponse{
		Result: &errorsv1.WatchResponse_Errors{
			Errors: &errorsv1.Errors{Items: items},
		},
		Kind: kind,
	}

	select {
	case ch <- response:
	default:
		// Channel full, skip this update.
	}
}

// convertErrorRecordToProto converts an internal error record to protobuf.
func convertErrorRecordToProto(err *ErrorRecord) *errorsv1.ErrorMessage {
	msg := &errorsv1.ErrorMessage{
		ErrorId:           err.ErrorID,
		CanonicalError:    err.MinerError,
		Summary:           err.Summary,
		CauseSummary:      err.CauseSummary,
		RecommendedAction: err.RecommendedAction,
		Severity:          err.Severity,
		FirstSeenAt:       timestamppb.New(err.FirstSeenAt),
		LastSeenAt:        timestamppb.New(err.LastSeenAt),
		VendorAttributes:  err.VendorAttributes,
		DeviceIdentifier:  err.DeviceID,
		Impact:            err.Impact,
	}

	if err.ClosedAt != nil {
		msg.ClosedAt = timestamppb.New(*err.ClosedAt)
	}

	if err.ComponentID != "" {
		msg.ComponentId = &err.ComponentID
	}

	return msg
}
