package sqlstores

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	fm "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	tm "github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	minermodels "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

var _ stores.DeviceStore = &SQLDeviceStore{}

// handleQueryError wraps database query errors with appropriate FleetError types.
// It converts sql.ErrNoRows to NotFoundError with a user-friendly message,
// and wraps unexpected database errors as InternalError with full error context.
// notFoundMsg should be a complete user-friendly message (e.g., "device not found with id=123").
// internalMsg should describe the operation context (e.g., "failed to query device").
func handleQueryError(err error, notFoundMsg, internalMsg string) error {
	if err == nil {
		return nil
	}
	if err == sql.ErrNoRows {
		return fleeterror.NewNotFoundError(notFoundMsg)
	}
	return fleeterror.NewInternalErrorf("%s: %v", internalMsg, err)
}

type SQLDeviceStore struct {
	SQLConnectionManager
}

func NewSQLDeviceStore(conn *sql.DB) *SQLDeviceStore {
	return &SQLDeviceStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLDeviceStore) getQueries(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

type deviceQueryCursor struct {
	ID       int64
	DeviceID int64
}

// encodeCursor encodes a Cursor struct to a base64 string
func (s *SQLDeviceStore) encodeCursor(c *deviceQueryCursor) string {
	if c == nil {
		return ""
	}
	raw := fmt.Sprintf("%d:%d", c.ID, c.DeviceID)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// decodeCursor decodes a base64 string to a Cursor struct
func (s *SQLDeviceStore) decodeCursor(encoded string) (deviceQueryCursor, error) {
	if encoded == "" {
		return deviceQueryCursor{}, nil
	}

	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return deviceQueryCursor{}, fleeterror.NewInvalidArgumentErrorf("invalid cursor encoding: %v", err)
	}

	var cursor deviceQueryCursor
	_, err = fmt.Sscanf(string(b), "%d:%d", &cursor.ID, &cursor.DeviceID)
	if err != nil {
		return deviceQueryCursor{}, fleeterror.NewInvalidArgumentErrorf("invalid cursor values: %v", err)
	}

	return cursor, nil
}

func (s *SQLDeviceStore) GetDeviceByDeviceIdentifier(ctx context.Context, identifier string, orgID int64) (*pb.Device, error) {
	device, err := s.getQueries(ctx).GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: identifier,
		OrgID:            orgID,
	})
	if err != nil {
		return nil, handleQueryError(err,
			fmt.Sprintf("device not found with identifier=%s org_id=%d", identifier, orgID),
			fmt.Sprintf("failed to query device with identifier=%s org_id=%d", identifier, orgID))
	}

	discoveredDevice, err := s.getQueries(ctx).GetDiscoveredDeviceByID(ctx, sqlc.GetDiscoveredDeviceByIDParams{
		ID:    device.DiscoveredDeviceID,
		OrgID: orgID,
	})

	if err != nil {
		return nil, handleQueryError(err,
			fmt.Sprintf("discovered device not found with id=%d org_id=%d", device.DiscoveredDeviceID, orgID),
			"failed to query discovered device")
	}

	result := &pb.Device{
		DeviceIdentifier: device.DeviceIdentifier,
		MacAddress:       device.MacAddress,
		SerialNumber:     device.SerialNumber.String,
		Model:            discoveredDevice.Model.String,
		Manufacturer:     discoveredDevice.Manufacturer.String,
		IpAddress:        discoveredDevice.IpAddress,
		Port:             discoveredDevice.Port,
		UrlScheme:        discoveredDevice.UrlScheme,
		Type:             discoveredDevice.Type,
	}

	return result, nil
}

func (s *SQLDeviceStore) UpdateDeviceInfo(ctx context.Context, device *pb.Device, orgID int64) error {
	err := s.getQueries(ctx).UpdateDeviceInfo(ctx, sqlc.UpdateDeviceInfoParams{
		MacAddress: device.MacAddress,
		SerialNumber: sql.NullString{
			String: device.SerialNumber,
			Valid:  device.SerialNumber != "",
		},
		DeviceIdentifier: device.DeviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update device info for identifier=%s org_id=%d: %v", device.DeviceIdentifier, orgID, err)
	}
	return nil
}

func (s *SQLDeviceStore) InsertDevice(ctx context.Context, device *pb.Device, orgID int64, discoveredDeviceIdentifier string) error {
	// Look up the discovered device database ID
	discoveredDevice, err := s.getQueries(ctx).GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{
		DeviceIdentifier: discoveredDeviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return handleQueryError(err,
			fmt.Sprintf("discovered device not found with identifier=%s org_id=%d", discoveredDeviceIdentifier, orgID),
			fmt.Sprintf("failed to query discovered device with identifier=%s org_id=%d", discoveredDeviceIdentifier, orgID))
	}

	_, err = s.getQueries(ctx).InsertDevice(ctx, sqlc.InsertDeviceParams{
		OrgID:              orgID,
		DiscoveredDeviceID: discoveredDevice.ID,
		DeviceIdentifier:   device.DeviceIdentifier,
		MacAddress:         device.MacAddress,
		SerialNumber:       sql.NullString{String: device.SerialNumber, Valid: device.SerialNumber != ""},
	})

	if err != nil {
		return err
	}

	return nil
}

func (s *SQLDeviceStore) UpsertMinerCredentials(ctx context.Context, device *pb.Device, orgID int64, usernameEnc string, passwordEnc *secrets.Text) error {
	dbDevice, err := s.getQueries(ctx).GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: device.DeviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return handleQueryError(err,
			fmt.Sprintf("device not found for credentials update with identifier=%s org_id=%d", device.DeviceIdentifier, orgID),
			"failed to query device")
	}
	err = s.getQueries(ctx).UpsertMinerCredentials(ctx, sqlc.UpsertMinerCredentialsParams{
		DeviceID:    dbDevice.ID,
		UsernameEnc: usernameEnc,
		PasswordEnc: passwordEnc.Value(),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to upsert miner credentials: %v", err)
	}
	return nil
}

func (s *SQLDeviceStore) UpsertDevicePairing(ctx context.Context, device *pb.Device, orgID int64, pairingStatus string) error {
	dbDevice, err := s.getQueries(ctx).GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: device.DeviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return handleQueryError(err,
			fmt.Sprintf("device not found for pairing update with identifier=%s org_id=%d", device.DeviceIdentifier, orgID),
			"failed to query device")
	}
	_, err = s.getQueries(ctx).UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{
		DeviceID:      dbDevice.ID,
		PairingStatus: sqlc.DevicePairingPairingStatus(pairingStatus),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to upsert device pairing: %v", err)
	}
	return nil
}

func (s *SQLDeviceStore) UpdateDevicePairingStatusByIdentifier(ctx context.Context, deviceIdentifier string, pairingStatus string) error {
	err := s.getQueries(ctx).UpdateDevicePairingStatusByIdentifier(ctx, sqlc.UpdateDevicePairingStatusByIdentifierParams{
		PairingStatus:    sqlc.DevicePairingPairingStatus(pairingStatus),
		DeviceIdentifier: deviceIdentifier,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update device pairing status for device %s: %v", deviceIdentifier, err)
	}
	return nil
}

func (s *SQLDeviceStore) GetMinerCredentials(ctx context.Context, device *pb.Device, orgID int64) (*pb.Credentials, error) {
	dbDevice, err := s.GetQueries(ctx).GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: device.DeviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return nil, handleQueryError(err,
			fmt.Sprintf("device not found for credentials retrieval with identifier=%s org_id=%d", device.DeviceIdentifier, orgID),
			"failed to query device")
	}
	credentials, err := s.GetQueries(ctx).GetMinerCredentialsByDeviceID(ctx, dbDevice.ID)
	if err != nil {
		return nil, handleQueryError(err,
			fmt.Sprintf("miner credentials not found for device_id=%d identifier=%s", dbDevice.ID, device.DeviceIdentifier),
			"failed to get miner credentials")
	}
	return &pb.Credentials{
		Username: credentials.UsernameEnc,
		Password: &credentials.PasswordEnc,
	}, nil
}

func (s *SQLDeviceStore) GetDeviceWithIPAssignment(ctx context.Context, deviceIdentifier string, orgID int64) (*discoverymodels.DiscoveredDevice, error) {
	q := s.GetQueries(ctx)

	device, err := q.GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return nil, handleQueryError(err,
			fmt.Sprintf("device not found for IP assignment with identifier=%s org_id=%d", deviceIdentifier, orgID),
			"failed to query device")
	}

	discoveredDevice, err := s.getQueries(ctx).GetDiscoveredDeviceByID(ctx, sqlc.GetDiscoveredDeviceByIDParams{
		ID:    device.DiscoveredDeviceID,
		OrgID: orgID,
	})
	if err != nil {
		return nil, handleQueryError(err,
			fmt.Sprintf("discovered device not found for device_identifier=%s org_id=%d", deviceIdentifier, orgID),
			fmt.Sprintf("failed to query discovered device for device_identifier=%s org_id=%d", deviceIdentifier, orgID))
	}

	return &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: device.DeviceIdentifier,
			MacAddress:       device.MacAddress,
			SerialNumber:     device.SerialNumber.String,
			Model:            discoveredDevice.Model.String,
			Manufacturer:     discoveredDevice.Manufacturer.String,
			IpAddress:        discoveredDevice.IpAddress,
			Port:             discoveredDevice.Port,
			UrlScheme:        discoveredDevice.UrlScheme,
			Type:             discoveredDevice.Type,
		},
		OrgID: orgID,
	}, nil
}

func (s *SQLDeviceStore) GetTotalPairedDevices(ctx context.Context, orgID int64, filter *stores.MinerFilter) (int64, error) {
	statusFilter, typeFilter := buildFilterParams(filter)

	return s.GetQueries(ctx).GetTotalPairedDevices(ctx, sqlc.GetTotalPairedDevicesParams{
		OrgID:        orgID,
		StatusFilter: statusFilter,
		TypeFilter:   typeFilter,
	})
}

func (s *SQLDeviceStore) GetTotalDevicesPendingAuth(ctx context.Context, orgID int64) (int64, error) {
	return s.GetQueries(ctx).GetTotalDevicesPendingAuth(ctx, orgID)
}

func (s *SQLDeviceStore) GetAllPairedDeviceIdentifiers(ctx context.Context) ([]models.DeviceIdentifier, error) {
	identifiers, err := s.GetQueries(ctx).GetAllPairedDeviceIdentifiers(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get all paired device identifiers: %v", err)
	}

	deviceIDs := make([]models.DeviceIdentifier, 0, len(identifiers))
	for _, identifier := range identifiers {
		deviceIDs = append(deviceIDs, models.NewDeviceIdentifierFromString(identifier))
	}

	return deviceIDs, nil
}

// GetMinerStateCounts returns counts of miners by operational state.
// The SQL query handles bucket assignment with status-first priority:
// Offline > Sleeping > Needs Attention > Hashing
func (s *SQLDeviceStore) GetMinerStateCounts(ctx context.Context, orgID int64, filter *stores.MinerFilter) (*tm.MinerStateCounts, error) {
	statusFilter, typeFilter := buildFilterParams(filter)

	counts, err := s.getQueries(ctx).CountMinersByState(ctx, sqlc.CountMinersByStateParams{
		OrgID:        orgID,
		StatusFilter: statusFilter,
		TypeFilter:   typeFilter,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to count miners by state: %v", err)
	}

	// Helper to convert interface{} to int32, handling both int64 and []byte from MySQL
	toInt32 := func(v interface{}) int32 {
		switch val := v.(type) {
		case int64:
			return int32(val) //nolint:gosec // Miner counts bounded by fleet size (<millions)
		case []byte:
			// MySQL SUM returns decimal as []byte, parse it with strconv for robustness
			if len(val) == 0 {
				return 0
			}
			parsed, err := strconv.ParseInt(string(val), 10, 64)
			if err != nil {
				// Log and return 0 as safe default. This should never happen with valid MySQL SUM results,
				// but if it does, returning 0 prevents cascading errors in fleet health calculations.
				// The error is logged so it can be monitored and investigated if it occurs in production.
				slog.Error("failed to parse miner count from database", "error", err, "raw_value", string(val))
				return 0
			}
			return int32(parsed) //nolint:gosec // Miner counts bounded by fleet size (<millions)
		default:
			return 0
		}
	}

	return &tm.MinerStateCounts{
		HashingCount:  toInt32(counts.HashingCount),
		BrokenCount:   toInt32(counts.BrokenCount),
		OfflineCount:  toInt32(counts.OfflineCount),
		SleepingCount: toInt32(counts.SleepingCount),
	}, nil
}

func (s *SQLDeviceStore) GetAvailableMinerTypes(ctx context.Context, orgID int64) ([]minermodels.Type, error) {
	typeStrings, err := s.getQueries(ctx).GetAvailableMinerTypes(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get available miner types: %v", err)
	}

	types := make([]minermodels.Type, 0, len(typeStrings))
	for _, typeStr := range typeStrings {
		// Skip "asic" type as it's ambiguous - devices with type="asic" will be
		// resolved to specific types (proto/antminer) via model field elsewhere
		if typeStr == "asic" {
			continue
		}
		minerType, err := minermodels.TypeFromString(typeStr)
		if err != nil {
			// Skip unknown types
			continue
		}
		types = append(types, minerType)
	}

	return types, nil
}

func buildFilterParams(filter *stores.MinerFilter) (statusFilter, typeFilter sql.NullString) {
	if filter != nil && len(filter.DeviceStatusFilter) > 0 {
		deviceFilter := make([]string, 0, len(filter.DeviceStatusFilter))
		for _, status := range filter.DeviceStatusFilter {
			deviceFilter = append(deviceFilter, status.String())
		}
		statusFilter = sql.NullString{String: strings.Join(deviceFilter, ","), Valid: true}
	}

	if filter != nil && len(filter.MinerType) > 0 {
		typeStrings := make([]string, len(filter.MinerType))
		for i, t := range filter.MinerType {
			typeStrings[i] = t.String()
		}
		typeFilter = sql.NullString{String: strings.Join(typeStrings, ","), Valid: true}
	}

	return statusFilter, typeFilter
}

func (s *SQLDeviceStore) UpsertDeviceStatus(ctx context.Context, deviceIdentifier models.DeviceIdentifier, status minermodels.MinerStatus, details string) error {
	sqlStatus := toDeviceStatus(status)
	deviceID, err := s.getQueries(ctx).GetDeviceIDByDeviceIdentifier(ctx, deviceIdentifier.String())
	if err != nil {
		return handleQueryError(err,
			fmt.Sprintf("device not found for status update with identifier=%s", deviceIdentifier),
			"failed to get device ID")
	}

	err = s.getQueries(ctx).UpsertDeviceStatus(ctx, sqlc.UpsertDeviceStatusParams{
		DeviceID:        deviceID,
		Status:          sqlStatus,
		StatusTimestamp: sql.NullTime{Time: time.Now(), Valid: true},
		StatusDetails:   sql.NullString{String: details, Valid: false},
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to upsert device status: %v", err)
	}
	return nil
}

func toDeviceStatus(status minermodels.MinerStatus) sqlc.DeviceStatusStatus {
	//nolint:exhaustive // We handle all known statuses, but we may not handle all possible statuses.
	switch status {
	case minermodels.MinerStatusActive:
		return sqlc.DeviceStatusStatusACTIVE
	case minermodels.MinerStatusOffline:
		return sqlc.DeviceStatusStatusOFFLINE
	case minermodels.MinerStatusInactive:
		return sqlc.DeviceStatusStatusINACTIVE
	case minermodels.MinerStatusMaintenance:
		return sqlc.DeviceStatusStatusMAINTENANCE
	case minermodels.MinerStatusError:
		return sqlc.DeviceStatusStatusERROR
	default:
		return sqlc.DeviceStatusStatusUNKNOWN
	}
}

func toMinerStatus(status sqlc.DeviceStatusStatus) minermodels.MinerStatus {
	//nolint:exhaustive // We handle all known statuses, but we may not handle all possible statuses.
	switch status {
	case sqlc.DeviceStatusStatusACTIVE:
		return minermodels.MinerStatusActive
	case sqlc.DeviceStatusStatusOFFLINE:
		return minermodels.MinerStatusOffline
	case sqlc.DeviceStatusStatusINACTIVE:
		return minermodels.MinerStatusInactive
	case sqlc.DeviceStatusStatusMAINTENANCE:
		return minermodels.MinerStatusMaintenance
	case sqlc.DeviceStatusStatusERROR:
		return minermodels.MinerStatusError
	default:
		return minermodels.MinerStatusUnknown
	}
}

func (s *SQLDeviceStore) GetDeviceStatusForDeviceIdentifiers(ctx context.Context, deviceIdentifiers []models.DeviceIdentifier) (map[models.DeviceIdentifier]minermodels.MinerStatus, error) {
	statusMap := make(map[models.DeviceIdentifier]minermodels.MinerStatus)

	if len(deviceIdentifiers) == 0 {
		return statusMap, nil
	}

	// Convert identifiers to string slice for the query
	ids := make([]string, len(deviceIdentifiers))
	for i, id := range deviceIdentifiers {
		ids[i] = id.String()
	}

	statuses, err := s.getQueries(ctx).GetDeviceStatusForDeviceIdentifiers(ctx, ids)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get device statuses: %v", err)
	}

	for _, status := range statuses {
		deviceID := models.NewDeviceIdentifierFromString(status.DeviceIdentifier)
		minerStatus := toMinerStatus(status.Status)
		statusMap[deviceID] = minerStatus
	}

	return statusMap, nil
}

// GetOfflineDevices retrieves a list of offline devices that need IP scanning
func (s *SQLDeviceStore) GetOfflineDevices(ctx context.Context, limit int) ([]stores.OfflineDeviceInfo, error) {
	const minLimit = 1
	// Validate limit parameter
	if limit < minLimit {
		return nil, fmt.Errorf("limit must be at least %d, got %d", minLimit, limit)
	}
	// Ensure limit is within valid int32 range to prevent overflow
	if limit > math.MaxInt32 {
		limit = math.MaxInt32
	}

	rows, err := s.getQueries(ctx).GetOfflineDevices(ctx, int32(limit)) // #nosec G115 -- overflow check above using math.MaxInt32
	if err != nil {
		return nil, fmt.Errorf("failed to get offline devices: %w", err)
	}

	offlineDevices := make([]stores.OfflineDeviceInfo, 0, len(rows))
	for _, row := range rows {
		device := stores.OfflineDeviceInfo{
			DeviceID:                   row.ID,
			DeviceIdentifier:           row.DeviceIdentifier,
			MacAddress:                 row.MacAddress,
			DeviceType:                 row.Type,
			OrgID:                      row.OrgID,
			DiscoveredDeviceIdentifier: row.DiscoveredDeviceIdentifier,
			LastKnownIP:                row.IpAddress,
			LastKnownPort:              row.Port,
			LastKnownURLScheme:         row.UrlScheme,
		}

		offlineDevices = append(offlineDevices, device)
	}

	return offlineDevices, nil
}

// ListMinerStateSnapshots retrieves both paired and unpaired devices using a unified query
func (s *SQLDeviceStore) ListMinerStateSnapshots(ctx context.Context, orgID int64, cursor string, pageSize int32, filter *stores.MinerFilter) ([]sqlc.ListMinerStateSnapshotsRow, string, int64, error) {
	// Decode cursor - now just a simple ID
	cursorID := int64(0)
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", 0, fleeterror.NewInvalidArgumentErrorf("invalid cursor: %v", err)
		}
		_, err = fmt.Sscanf(string(decoded), "%d", &cursorID)
		if err != nil {
			return nil, "", 0, fleeterror.NewInvalidArgumentErrorf("invalid cursor format: %v", err)
		}
	}

	// Build filter parameters
	var statusFilter interface{}
	var statusValues []sqlc.DeviceStatusStatus
	needsAttentionFilter := false
	if filter != nil && len(filter.DeviceStatusFilter) > 0 {
		statusFilter = true // Non-null indicates filter is active
		for _, status := range filter.DeviceStatusFilter {
			statusValues = append(statusValues, toDeviceStatus(status))
			// Special case: if ERROR status is requested, also include AUTHENTICATION_NEEDED devices
			// This implements "needs attention" = ERROR status OR AUTHENTICATION_NEEDED pairing
			if status == minermodels.MinerStatusError {
				needsAttentionFilter = true
			}
		}
	}

	var typeFilter interface{}
	var typeValues []string
	if filter != nil && len(filter.MinerType) > 0 {
		typeFilter = true // Non-null indicates filter is active
		for _, t := range filter.MinerType {
			typeValues = append(typeValues, t.String())
		}
	}

	// Parse pairing status filter - convert proto enums to database enum strings
	// Pass the list of statuses directly to SQL instead of boolean flags
	var pairingStatusFilter interface{}
	var pairingStatusValues []sqlc.DevicePairingPairingStatus

	if filter != nil && len(filter.PairingStatuses) > 0 {
		// Filter is provided - convert proto enums to sqlc enums
		pairingStatusFilter = true // Non-null indicates filter is active
		for _, status := range filter.PairingStatuses {
			switch status {
			case fm.PairingStatus_PAIRING_STATUS_UNSPECIFIED:
				// UNSPECIFIED means "return all" - skip adding to filter
				// If this is the only status, clear the filter entirely
				continue
			case fm.PairingStatus_PAIRING_STATUS_PAIRED:
				pairingStatusValues = append(pairingStatusValues, sqlc.DevicePairingPairingStatusPAIRED)
			case fm.PairingStatus_PAIRING_STATUS_UNPAIRED:
				pairingStatusValues = append(pairingStatusValues, sqlc.DevicePairingPairingStatusUNPAIRED)
			case fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED:
				pairingStatusValues = append(pairingStatusValues, sqlc.DevicePairingPairingStatusAUTHENTICATIONNEEDED)
			case fm.PairingStatus_PAIRING_STATUS_PENDING:
				pairingStatusValues = append(pairingStatusValues, sqlc.DevicePairingPairingStatusPENDING)
			case fm.PairingStatus_PAIRING_STATUS_FAILED:
				pairingStatusValues = append(pairingStatusValues, sqlc.DevicePairingPairingStatusFAILED)
			default:
				// Unknown pairing status - skip it rather than fail the query
				// This provides forward compatibility if new statuses are added
				continue
			}
		}

		// If no valid pairing statuses were added (all were UNSPECIFIED or unknown),
		// clear the filter to return all devices
		if len(pairingStatusValues) == 0 {
			pairingStatusFilter = nil
		}
	}

	// If no pairing statuses provided at all, filter remains nil (return all)

	// Build component type filter parameters
	var errorComponentTypesFilter interface{}
	var errorComponentTypeValues []sql.NullInt32
	if filter != nil && len(filter.ErrorComponentTypes) > 0 {
		errorComponentTypesFilter = true // Non-null indicates filter is active
		errorComponentTypeValues = make([]sql.NullInt32, len(filter.ErrorComponentTypes))
		for i, ct := range filter.ErrorComponentTypes {
			// #nosec G115 -- ComponentType enum bounded (0-6), safe for int32
			errorComponentTypeValues[i] = sql.NullInt32{Int32: int32(ct), Valid: true}
		}
	}

	// Call unified query with all filter parameters
	// Pass needsAttentionFilter to SQL for special OR logic handling
	// Pass errorComponentTypesFilter to SQL for component type filtering
	rows, err := s.getQueries(ctx).ListMinerStateSnapshots(ctx, sqlc.ListMinerStateSnapshotsParams{
		OrgID:                     orgID,
		CursorID:                  sql.NullInt64{Int64: cursorID, Valid: cursorID > 0},
		StatusFilter:              statusFilter,
		StatusValues:              statusValues,
		TypeFilter:                typeFilter,
		TypeValues:                typeValues,
		PairingStatusFilter:       pairingStatusFilter,
		PairingStatusValues:       pairingStatusValues,
		NeedsAttentionFilter:      sql.NullBool{Bool: needsAttentionFilter, Valid: needsAttentionFilter},
		ErrorComponentTypesFilter: errorComponentTypesFilter,
		ErrorComponentTypeValues:  errorComponentTypeValues,
		Limit:                     pageSize + 1,
	})
	if err != nil {
		return nil, "", 0, fleeterror.NewInternalErrorf("failed to list miner state snapshots: %v", err)
	}

	// Process results
	hasMore := len(rows) > int(pageSize)
	if hasMore {
		rows = rows[:pageSize]
	}

	// Build next cursor - simple encoding of just the ID
	var nextCursor string
	if hasMore && len(rows) > 0 {
		lastRow := rows[len(rows)-1]
		nextCursor = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", lastRow.CursorID)))
	}

	// Get total count with same filter parameters (including needs attention OR logic)
	total, err := s.getQueries(ctx).GetTotalMinerStateSnapshots(ctx, sqlc.GetTotalMinerStateSnapshotsParams{
		OrgID:                     orgID,
		StatusFilter:              statusFilter,
		StatusValues:              statusValues,
		TypeFilter:                typeFilter,
		TypeValues:                typeValues,
		PairingStatusFilter:       pairingStatusFilter,
		PairingStatusValues:       pairingStatusValues,
		NeedsAttentionFilter:      sql.NullBool{Bool: needsAttentionFilter, Valid: needsAttentionFilter},
		ErrorComponentTypesFilter: errorComponentTypesFilter,
		ErrorComponentTypeValues:  errorComponentTypeValues,
	})
	if err != nil {
		return nil, "", 0, fleeterror.NewInternalErrorf("failed to get total count: %v", err)
	}

	return rows, nextCursor, total, nil
}

// AllDevicesBelongToOrg returns true if all provided device identifiers belong to the specified organization.
// Used for authorization checks - returns false if any device is not owned by the org.
func (s *SQLDeviceStore) AllDevicesBelongToOrg(ctx context.Context, deviceIdentifiers []string, orgID int64) (bool, error) {
	if len(deviceIdentifiers) == 0 {
		return true, nil
	}

	return s.getQueries(ctx).AllDevicesBelongToOrg(ctx, sqlc.AllDevicesBelongToOrgParams{
		ExpectedCount:     len(deviceIdentifiers),
		DeviceIdentifiers: deviceIdentifiers,
		OrgID:             orgID,
	})
}
