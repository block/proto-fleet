package sqlstores

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"
	"math"
	"sort"
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

const (
	// ambiguousASICType is skipped when returning available miner types.
	// Devices with type="asic" are resolved to specific types (proto/antminer)
	// via the model field elsewhere.
	ambiguousASICType = "asic"
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
		PairingStatus: sqlc.PairingStatusEnum(pairingStatus),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to upsert device pairing: %v", err)
	}
	return nil
}

func (s *SQLDeviceStore) UpdateDevicePairingStatusByIdentifier(ctx context.Context, deviceIdentifier string, pairingStatus string) error {
	err := s.getQueries(ctx).UpdateDevicePairingStatusByIdentifier(ctx, sqlc.UpdateDevicePairingStatusByIdentifierParams{
		PairingStatus:    sqlc.PairingStatusEnum(pairingStatus),
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
	statusFilter, modelFilter := buildFilterParams(filter)

	return s.GetQueries(ctx).GetTotalPairedDevices(ctx, sqlc.GetTotalPairedDevicesParams{
		OrgID:        orgID,
		StatusFilter: statusFilter,
		ModelFilter:  modelFilter,
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
		deviceIDs = append(deviceIDs, models.DeviceIdentifier(identifier))
	}

	return deviceIDs, nil
}

// GetMinerStateCounts returns counts of miners by operational state.
// The SQL query handles bucket assignment with status-first priority:
// Offline > Sleeping > Needs Attention > Hashing
func (s *SQLDeviceStore) GetMinerStateCounts(ctx context.Context, orgID int64, filter *stores.MinerFilter) (*tm.MinerStateCounts, error) {
	statusFilter, modelFilter := buildFilterParams(filter)

	counts, err := s.getQueries(ctx).CountMinersByState(ctx, sqlc.CountMinersByStateParams{
		OrgID:        orgID,
		StatusFilter: statusFilter,
		ModelFilter:  modelFilter,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to count miners by state: %v", err)
	}

	return &tm.MinerStateCounts{
		HashingCount:  int32(counts.HashingCount),  //nolint:gosec // Miner counts bounded by fleet size (<millions)
		BrokenCount:   int32(counts.BrokenCount),   //nolint:gosec // Miner counts bounded by fleet size (<millions)
		OfflineCount:  int32(counts.OfflineCount),  //nolint:gosec // Miner counts bounded by fleet size (<millions)
		SleepingCount: int32(counts.SleepingCount), //nolint:gosec // Miner counts bounded by fleet size (<millions)
	}, nil
}

func (s *SQLDeviceStore) GetAvailableModels(ctx context.Context, orgID int64) ([]string, error) {
	nullModels, err := s.getQueries(ctx).GetAvailableModels(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get available models: %v", err)
	}
	models := make([]string, 0, len(nullModels))
	for _, m := range nullModels {
		if m.Valid && m.String != "" {
			models = append(models, m.String)
		}
	}
	return models, nil
}

func buildFilterParams(filter *stores.MinerFilter) (statusFilter, modelFilter sql.NullString) {
	if filter != nil && len(filter.DeviceStatusFilter) > 0 {
		deviceFilter := make([]string, 0, len(filter.DeviceStatusFilter))
		for _, status := range filter.DeviceStatusFilter {
			deviceFilter = append(deviceFilter, string(toDeviceStatus(status)))
		}
		statusFilter = sql.NullString{String: strings.Join(deviceFilter, ","), Valid: true}
	}

	if filter != nil && len(filter.ModelNames) > 0 {
		modelFilter = sql.NullString{String: strings.Join(filter.ModelNames, ","), Valid: true}
	}

	return statusFilter, modelFilter
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

// UpsertDeviceStatuses upserts multiple device statuses in a single bulk query.
func (s *SQLDeviceStore) UpsertDeviceStatuses(ctx context.Context, updates []stores.DeviceStatusUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	// Batch lookup: get device IDs for all identifiers
	identifiers := make([]string, len(updates))
	for i, u := range updates {
		identifiers[i] = u.DeviceIdentifier.String()
	}

	rows, err := s.getQueries(ctx).GetDeviceIDsWithIdentifiers(ctx, identifiers)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to get device IDs for status update: %v", err)
	}

	idMap := make(map[string]int64, len(rows))
	for _, row := range rows {
		idMap[row.DeviceIdentifier] = row.ID
	}

	// Collect valid updates with their device IDs, deduplicating by device_id.
	// PostgreSQL's ON CONFLICT DO UPDATE cannot affect the same row twice in one INSERT,
	// so we keep only the last update for each device_id (last-write-wins semantics).
	type deviceStatusUpdateWithID struct {
		deviceID int64
		update   stores.DeviceStatusUpdate
	}
	dedupedByDeviceID := make(map[int64]deviceStatusUpdateWithID)
	notFoundCount := 0
	for _, u := range updates {
		deviceID, found := idMap[u.DeviceIdentifier.String()]
		if !found {
			notFoundCount++
			continue
		}
		dedupedByDeviceID[deviceID] = deviceStatusUpdateWithID{deviceID: deviceID, update: u}
	}

	validUpdates := make([]deviceStatusUpdateWithID, 0, len(dedupedByDeviceID))
	for _, v := range dedupedByDeviceID {
		validUpdates = append(validUpdates, v)
	}
	if notFoundCount > 0 {
		slog.Warn("some devices not found for status update",
			"not_found", notFoundCount,
			"total", len(updates),
			"succeeded", len(validUpdates))
	}

	if len(validUpdates) == 0 {
		return fleeterror.NewInternalErrorf("all %d devices not found for status update", len(updates))
	}

	// Sort by device_id for consistent lock ordering. This prevents deadlocks
	// with queries that scan device_status in index order (e.g., CloseStaleErrors
	// EXISTS subquery which acquires shared locks during its scan).
	sort.Slice(validUpdates, func(i, j int) bool {
		return validUpdates[i].deviceID < validUpdates[j].deviceID
	})

	// Build args in sorted order
	now := time.Now()
	args := make([]any, 0, len(validUpdates)*4)
	for _, v := range validUpdates {
		args = append(args, v.deviceID, toDeviceStatus(v.update.Status), now, "")
	}

	query := buildDeviceStatusBulkUpsert(len(validUpdates))
	_, err = s.conn.ExecContext(ctx, query, args...)
	if err != nil {
		return fleeterror.NewInternalErrorf("bulk status upsert failed: %v", err)
	}
	return nil
}

// buildDeviceStatusBulkUpsert builds a bulk INSERT ... ON CONFLICT DO UPDATE query for PostgreSQL.
//
// We use a manual query instead of:
//   - N individual queries in a transaction: Creates long-running transactions that hold
//     locks and exhaust DB connections under load.
//   - sqlc :copyfrom: Uses COPY which doesn't support ON CONFLICT DO UPDATE,
//     requiring DELETE+INSERT which is slower and not atomic.
//
// A single bulk INSERT with ON CONFLICT DO UPDATE is both fast (1 round-trip) and atomic.
func buildDeviceStatusBulkUpsert(rowCount int) string {
	var b strings.Builder
	paramNum := 1
	for i := range rowCount {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "($%d, $%d, $%d, $%d)", paramNum, paramNum+1, paramNum+2, paramNum+3)
		paramNum += 4
	}

	return fmt.Sprintf(
		"INSERT INTO device_status (device_id, status, status_timestamp, status_details) VALUES %s "+
			"ON CONFLICT (device_id) DO UPDATE SET "+
			"status = EXCLUDED.status, "+
			"status_timestamp = EXCLUDED.status_timestamp, "+
			"status_details = EXCLUDED.status_details",
		b.String(),
	)
}

func toDeviceStatus(status minermodels.MinerStatus) sqlc.DeviceStatusEnum {
	//nolint:exhaustive // We handle all known statuses, but we may not handle all possible statuses.
	switch status {
	case minermodels.MinerStatusActive:
		return sqlc.DeviceStatusEnumACTIVE
	case minermodels.MinerStatusOffline:
		return sqlc.DeviceStatusEnumOFFLINE
	case minermodels.MinerStatusInactive:
		return sqlc.DeviceStatusEnumINACTIVE
	case minermodels.MinerStatusMaintenance:
		return sqlc.DeviceStatusEnumMAINTENANCE
	case minermodels.MinerStatusError:
		return sqlc.DeviceStatusEnumERROR
	case minermodels.MinerStatusNeedsMiningPool:
		return sqlc.DeviceStatusEnumNEEDSMININGPOOL
	default:
		return sqlc.DeviceStatusEnumUNKNOWN
	}
}

func toMinerStatus(status sqlc.DeviceStatusEnum) minermodels.MinerStatus {
	//nolint:exhaustive // We handle all known statuses, but we may not handle all possible statuses.
	switch status {
	case sqlc.DeviceStatusEnumACTIVE:
		return minermodels.MinerStatusActive
	case sqlc.DeviceStatusEnumOFFLINE:
		return minermodels.MinerStatusOffline
	case sqlc.DeviceStatusEnumINACTIVE:
		return minermodels.MinerStatusInactive
	case sqlc.DeviceStatusEnumMAINTENANCE:
		return minermodels.MinerStatusMaintenance
	case sqlc.DeviceStatusEnumERROR:
		return minermodels.MinerStatusError
	case sqlc.DeviceStatusEnumNEEDSMININGPOOL:
		return minermodels.MinerStatusNeedsMiningPool
	default:
		return minermodels.MinerStatusUnknown
	}
}

// ProtoDeviceStatusToSQL converts protobuf DeviceStatus enum to sqlc DeviceStatusStatus
// Exported helper for use across packages (e.g., command service)
func ProtoDeviceStatusToSQL(status fm.DeviceStatus) sqlc.DeviceStatusEnum {
	switch status {
	case fm.DeviceStatus_DEVICE_STATUS_UNSPECIFIED:
		return sqlc.DeviceStatusEnumUNKNOWN
	case fm.DeviceStatus_DEVICE_STATUS_ONLINE:
		return sqlc.DeviceStatusEnumACTIVE
	case fm.DeviceStatus_DEVICE_STATUS_OFFLINE:
		return sqlc.DeviceStatusEnumOFFLINE
	case fm.DeviceStatus_DEVICE_STATUS_MAINTENANCE:
		return sqlc.DeviceStatusEnumMAINTENANCE
	case fm.DeviceStatus_DEVICE_STATUS_ERROR:
		return sqlc.DeviceStatusEnumERROR
	case fm.DeviceStatus_DEVICE_STATUS_INACTIVE:
		return sqlc.DeviceStatusEnumINACTIVE
	case fm.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL:
		return sqlc.DeviceStatusEnumNEEDSMININGPOOL
	default:
		return sqlc.DeviceStatusEnumUNKNOWN
	}
}

// ProtoPairingStatusToSQL converts protobuf PairingStatus enum to sqlc DevicePairingPairingStatus
// Exported helper for use across packages (e.g., command service)
func ProtoPairingStatusToSQL(status fm.PairingStatus) sqlc.PairingStatusEnum {
	switch status {
	case fm.PairingStatus_PAIRING_STATUS_UNSPECIFIED:
		return sqlc.PairingStatusEnumUNPAIRED
	case fm.PairingStatus_PAIRING_STATUS_PAIRED:
		return sqlc.PairingStatusEnumPAIRED
	case fm.PairingStatus_PAIRING_STATUS_UNPAIRED:
		return sqlc.PairingStatusEnumUNPAIRED
	case fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED:
		return sqlc.PairingStatusEnumAUTHENTICATIONNEEDED
	case fm.PairingStatus_PAIRING_STATUS_PENDING:
		return sqlc.PairingStatusEnumPENDING
	case fm.PairingStatus_PAIRING_STATUS_FAILED:
		return sqlc.PairingStatusEnumFAILED
	default:
		return sqlc.PairingStatusEnumUNPAIRED
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
		deviceID := models.DeviceIdentifier(status.DeviceIdentifier)
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

// ListMinerStateSnapshots retrieves both paired and unpaired devices using a query builder.
// Supports sorted pagination using keyset pagination with cursor encoding.
func (s *SQLDeviceStore) ListMinerStateSnapshots(ctx context.Context, orgID int64, cursor string, pageSize int32, filter *stores.MinerFilter, sortConfig *stores.SortConfig) ([]sqlc.ListMinerStateSnapshotsRow, string, int64, error) {
	// Decode cursor - sorted cursor format
	decodedCursor, err := decodeSortedCursor(cursor, sortConfig)
	if err != nil {
		return nil, "", 0, err
	}

	// Build filter parameters
	fp := buildMinerFilterParams(filter)

	// Execute query with filters and sorting
	rows, err := s.executeListQuery(ctx, orgID, decodedCursor, pageSize, fp, sortConfig)
	if err != nil {
		return nil, "", 0, err
	}

	// Process results
	hasMore := len(rows) > int(pageSize)
	if hasMore {
		rows = rows[:pageSize]
	}

	// Build next cursor - sorted cursor encoding
	var nextCursor string
	if hasMore && len(rows) > 0 {
		lastRow := rows[len(rows)-1]
		sortField := stores.SortFieldUnspecified
		sortDir := stores.SortDirectionUnspecified
		if sortConfig != nil {
			sortField = sortConfig.Field
			sortDir = sortConfig.Direction
		}
		nextCursor = encodeSortedCursor(&sortedCursor{
			SortField:     sortField,
			SortDirection: sortDir,
			SortValue:     extractSortValueForCursorFromRow(lastRow, sortConfig),
			CursorID:      lastRow.CursorID,
		})
	}

	// Get total count with same filters (still uses SQLC for count query)
	total, err := s.getQueries(ctx).GetTotalMinerStateSnapshots(ctx, sqlc.GetTotalMinerStateSnapshotsParams{
		OrgID:                     orgID,
		StatusFilter:              fp.statusFilter,
		StatusValues:              fp.statusValues,
		ModelFilter:               fp.modelFilter,
		ModelValues:               fp.modelValues,
		PairingStatusFilter:       fp.pairingStatusFilter,
		PairingStatusValues:       fp.pairingStatusValues,
		NeedsAttentionFilter:      sql.NullBool{Bool: fp.needsAttentionFilter, Valid: fp.needsAttentionFilter},
		ErrorComponentTypesFilter: fp.errorComponentTypesFilter,
		ErrorComponentTypeValues:  fp.errorComponentTypeValues,
	})
	if err != nil {
		return nil, "", 0, fleeterror.NewInternalErrorf("failed to get total count: %v", err)
	}

	// Convert to SQLC row type for return
	result := make([]sqlc.ListMinerStateSnapshotsRow, len(rows))
	for i, row := range rows {
		result[i] = row.ListMinerStateSnapshotsRow
	}

	return result, nextCursor, total, nil
}

// minerStateRow extends the SQLC row with optional telemetry sort value.
type minerStateRow struct {
	sqlc.ListMinerStateSnapshotsRow
	SortValue sql.NullFloat64
}

// executeListQuery builds and executes the miner list query with all filters and sorting.
func (s *SQLDeviceStore) executeListQuery(ctx context.Context, orgID int64, cursor *sortedCursor, pageSize int32, fp minerFilterParams, sortConfig *stores.SortConfig) ([]minerStateRow, error) {
	query, args := s.buildListQuerySQL(orgID, cursor, pageSize, fp, sortConfig)

	sqlRows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list miner state snapshots: %v", err)
	}
	defer sqlRows.Close()

	rows := make([]minerStateRow, 0, pageSize+1)
	for sqlRows.Next() {
		var row minerStateRow
		err = sqlRows.Scan(
			&row.DeviceIdentifier,
			&row.MacAddress,
			&row.SerialNumber,
			&row.Model,
			&row.Manufacturer,
			&row.Type,
			&row.FirmwareVersion,
			&row.DeviceStatus,
			&row.StatusTimestamp,
			&row.StatusDetails,
			&row.IpAddress,
			&row.Port,
			&row.UrlScheme,
			&row.PairingStatus,
			&row.CursorID,
			&row.DeviceID,
			&row.SortValue,
		)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to list miner state snapshots: %v", err)
		}
		rows = append(rows, row)
	}

	if err := sqlRows.Err(); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list miner state snapshots: %v", err)
	}

	return rows, nil
}

// buildListQuerySQL builds the SQL query for listing miners with filters and sorting.
func (s *SQLDeviceStore) buildListQuerySQL(orgID int64, cursor *sortedCursor, pageSize int32, fp minerFilterParams, sortConfig *stores.SortConfig) (string, []any) {
	var sb strings.Builder
	args := []any{orgID}
	argNum := 2

	isTelemetrySort := sortConfig != nil && sortConfig.IsTelemetrySort()

	// Add CTE for telemetry sorting
	if isTelemetrySort {
		metricExpr := getTelemetryMetricExpression(sortConfig.Field)
		fmt.Fprintf(&sb, latestMetricsCTE+" ", metricExpr)
	}

	// Base query with appropriate sort column
	if isTelemetrySort {
		sb.WriteString(minerBaseQueryWithSortValue("latest_metrics.sort_value"))
		sb.WriteString(" " + minerTelemetryJoin)
		sb.WriteString(minerWhereClause)
	} else {
		sb.WriteString(minerBaseQuery)
	}

	// Keyset pagination condition
	keysetSQL, keysetArgs := buildKeysetSQL(cursor, sortConfig, argNum)
	if keysetSQL != "" {
		sb.WriteString(" " + keysetSQL)
		args = append(args, keysetArgs...)
		argNum += len(keysetArgs)
	}

	// Apply filters
	args, argNum = appendFilterSQL(&sb, args, argNum, orgID, fp)

	// ORDER BY and LIMIT
	sb.WriteString(" " + buildSortOrderClause(sortConfig))
	fmt.Fprintf(&sb, " LIMIT $%d", argNum)
	args = append(args, pageSize+1)

	return sb.String(), args
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

func (s *SQLDeviceStore) UpdateFirmwareVersion(ctx context.Context, deviceIdentifier models.DeviceIdentifier, firmwareVersion string) error {
	err := s.getQueries(ctx).UpdateDiscoveredDeviceFirmwareVersion(ctx, sqlc.UpdateDiscoveredDeviceFirmwareVersionParams{
		DeviceIdentifier: string(deviceIdentifier),
		FirmwareVersion:  sql.NullString{String: firmwareVersion, Valid: firmwareVersion != ""},
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update firmware version for device %s: %v", deviceIdentifier, err)
	}
	return nil
}
