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

	"github.com/lib/pq"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	tm "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
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
	}

	return result, nil
}

func (s *SQLDeviceStore) UpdateDeviceInfo(ctx context.Context, device *pb.Device, orgID int64) error {
	err := s.getQueries(ctx).UpdateDeviceInfo(ctx, sqlc.UpdateDeviceInfoParams{
		MacAddress: networking.NormalizeMAC(device.MacAddress),
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
		MacAddress:         networking.NormalizeMAC(device.MacAddress),
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

func (s *SQLDeviceStore) GetDevicePairingStatusByIdentifier(ctx context.Context, deviceIdentifier string, orgID int64) (string, error) {
	device, err := s.getQueries(ctx).GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return "", handleQueryError(err,
			fmt.Sprintf("device not found for pairing status with identifier=%s org_id=%d", deviceIdentifier, orgID),
			"failed to query device")
	}

	pairingStatus, err := s.getQueries(ctx).GetDevicePairingStatusByDeviceDatabaseID(ctx, device.ID)
	if err != nil {
		return "", handleQueryError(err,
			fmt.Sprintf("pairing status not found for device_id=%d identifier=%s", device.ID, deviceIdentifier),
			"failed to query device pairing status")
	}

	return string(pairingStatus), nil
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
		},
		OrgID: orgID,
	}, nil
}

func (s *SQLDeviceStore) GetTotalPairedDevices(ctx context.Context, orgID int64, filter *stores.MinerFilter) (int64, error) {
	statusFilter, modelFilter, _ := buildFilterParams(filter)

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
// Bucket rules live in CountMinersByState in server/sqlc/queries/device.sql
// and mirror MinerStatus.tsx (auth-needed overrides sleeping).
func (s *SQLDeviceStore) GetMinerStateCounts(ctx context.Context, orgID int64, filter *stores.MinerFilter) (*tm.MinerStateCounts, error) {
	fp := buildMinerFilterParams(filter)

	counts, err := s.getQueries(ctx).CountMinersByState(ctx, sqlc.CountMinersByStateParams{
		OrgID:                   orgID,
		StatusFilter:            fp.statusFilter,
		StatusValues:            fp.statusValues,
		NeedsAttentionFilter:    sql.NullBool{Bool: fp.needsAttentionFilter, Valid: fp.needsAttentionFilter},
		IncludeNullStatusFilter: sql.NullBool{Bool: fp.includeNullStatus, Valid: fp.includeNullStatus},
		ModelFilter:             fp.modelFilter,
		ModelValues:             fp.modelValues,
		DeviceIdentifiersFilter: fp.deviceIdentifiersFilter,
		DeviceIdentifierValues:  fp.deviceIdentifierValues,
		GroupIdsFilter:          fp.groupIDsFilter,
		GroupIDValues:           fp.groupIDValues,
		RackIdsFilter:           fp.rackIDsFilter,
		RackIDValues:            fp.rackIDValues,
		FirmwareVersionsFilter:  fp.firmwareVersionsFilter,
		FirmwareVersionValues:   fp.firmwareVersionValues,
		ZonesFilter:             fp.zonesFilter,
		ZoneValues:              fp.zoneValues,
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

func (s *SQLDeviceStore) GetAvailableFirmwareVersions(ctx context.Context, orgID int64) ([]string, error) {
	nullVersions, err := s.getQueries(ctx).GetAvailableFirmwareVersions(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get available firmware versions: %v", err)
	}
	versions := make([]string, 0, len(nullVersions))
	for _, v := range nullVersions {
		if v.Valid && v.String != "" {
			versions = append(versions, v.String)
		}
	}
	return versions, nil
}

func (s *SQLDeviceStore) GetMinerModelGroups(ctx context.Context, orgID int64, filter *stores.MinerFilter) ([]stores.MinerModelGroupResult, error) {
	statusFilter, modelFilter, _ := buildFilterParams(filter)

	rows, err := s.getQueries(ctx).GetMinerModelGroups(ctx, sqlc.GetMinerModelGroupsParams{
		OrgID:        orgID,
		ModelFilter:  modelFilter,
		StatusFilter: statusFilter,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get miner model groups: %v", err)
	}

	results := make([]stores.MinerModelGroupResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, stores.MinerModelGroupResult{
			Model:        row.Model.String,
			Manufacturer: row.Manufacturer.String,
			Count:        row.Count,
		})
	}
	return results, nil
}

func buildFilterParams(filter *stores.MinerFilter) (statusFilter, modelFilter, deviceIdentifiersFilter sql.NullString) {
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

	if filter != nil && len(filter.DeviceIdentifiers) > 0 {
		deviceIdentifiersFilter = sql.NullString{String: strings.Join(filter.DeviceIdentifiers, ","), Valid: true}
	}

	return statusFilter, modelFilter, deviceIdentifiersFilter
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
// The WHERE clause prevents telemetry status writes from overwriting firmware update
// states (UPDATING, REBOOT_REQUIRED) that are managed by the command execution service.
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
			"status_details = EXCLUDED.status_details "+
			"WHERE device_status.status NOT IN ('UPDATING', 'REBOOT_REQUIRED')",
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
	case minermodels.MinerStatusUpdating:
		return sqlc.DeviceStatusEnumUPDATING
	case minermodels.MinerStatusRebootRequired:
		return sqlc.DeviceStatusEnumREBOOTREQUIRED
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
	case sqlc.DeviceStatusEnumUPDATING:
		return minermodels.MinerStatusUpdating
	case sqlc.DeviceStatusEnumREBOOTREQUIRED:
		return minermodels.MinerStatusRebootRequired
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
	case fm.DeviceStatus_DEVICE_STATUS_UPDATING:
		return sqlc.DeviceStatusEnumUPDATING
	case fm.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED:
		return sqlc.DeviceStatusEnumREBOOTREQUIRED
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
			DriverName:                 row.DriverName,
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

// GetKnownSubnets retrieves unique subnets inferred from paired devices' last known IP addresses.
func (s *SQLDeviceStore) GetKnownSubnets(ctx context.Context, orgID int64, maskBits int, isIPv4 bool) ([]string, error) {
	maxBits := 128
	if isIPv4 {
		maxBits = 32
	}
	if maskBits < 0 || maskBits > maxBits {
		return nil, fmt.Errorf("maskBits must be between 0 and %d for %s, got %d",
			maxBits, map[bool]string{true: "IPv4", false: "IPv6"}[isIPv4], maskBits)
	}

	rows, err := s.getQueries(ctx).GetKnownSubnets(ctx, sqlc.GetKnownSubnetsParams{
		OrgID:    orgID,
		MaskBits: int32(maskBits), // #nosec G115 -- validated above to fit within int32 range
		IsIpv4:   isIPv4,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get known subnets: %w", err)
	}

	return rows, nil
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
		IncludeNullStatusFilter:   sql.NullBool{Bool: fp.includeNullStatus, Valid: fp.includeNullStatus},
		ErrorComponentTypesFilter: fp.errorComponentTypesFilter,
		ErrorComponentTypeValues:  fp.errorComponentTypeValues,
		GroupIdsFilter:            fp.groupIDsFilter,
		GroupIDValues:             fp.groupIDValues,
		RackIdsFilter:             fp.rackIDsFilter,
		RackIDValues:              fp.rackIDValues,
		FirmwareVersionsFilter:    fp.firmwareVersionsFilter,
		FirmwareVersionValues:     fp.firmwareVersionValues,
		ZonesFilter:               fp.zonesFilter,
		ZoneValues:                fp.zoneValues,
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
			&row.FirmwareVersion,
			&row.WorkerName,
			&row.DeviceStatus,
			&row.StatusTimestamp,
			&row.StatusDetails,
			&row.IpAddress,
			&row.Port,
			&row.UrlScheme,
			&row.PairingStatus,
			&row.CursorID,
			&row.DeviceID,
			&row.DriverName,
			&row.CustomName,
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

// SoftDeleteDevices verifies ownership and soft-deletes devices and their associated
// discovered_device records in a single transaction to prevent TOCTOU races.
// Returns a Forbidden error if any device does not belong to the specified org.
func (s *SQLDeviceStore) SoftDeleteDevices(ctx context.Context, deviceIdentifiers []string, orgID int64) (int64, error) {
	if len(deviceIdentifiers) == 0 {
		return 0, nil
	}

	deletedCount, err := db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (int64, error) {
		allBelong, err := q.AllDevicesBelongToOrg(ctx, sqlc.AllDevicesBelongToOrgParams{
			ExpectedCount:     len(deviceIdentifiers),
			DeviceIdentifiers: deviceIdentifiers,
			OrgID:             orgID,
		})
		if err != nil {
			return 0, fleeterror.NewInternalErrorf("failed to validate device ownership: %v", err)
		}
		if !allBelong {
			return 0, fleeterror.NewForbiddenError("access denied to one or more requested devices")
		}

		count, err := q.SoftDeleteDevices(ctx, sqlc.SoftDeleteDevicesParams{
			DeviceIdentifiers: deviceIdentifiers,
			OrgID:             orgID,
		})
		if err != nil {
			return 0, fleeterror.NewInternalErrorf("failed to soft-delete devices: %v", err)
		}

		err = q.SoftDeleteDiscoveredDevicesForDeletedDevices(ctx, sqlc.SoftDeleteDiscoveredDevicesForDeletedDevicesParams{
			DeviceIdentifiers: deviceIdentifiers,
			OrgID:             orgID,
		})
		if err != nil {
			return 0, fleeterror.NewInternalErrorf("failed to soft-delete discovered devices: %v", err)
		}

		return count, nil
	})
	if err != nil {
		return 0, err
	}

	return deletedCount, nil
}

// GetDeviceIdentifiersByOrgWithFilter returns device identifiers filtered by optional
// pairing status, device status, model, and error component types. Uses appendFilterSQL
// (the same dynamic filter logic as the list view) to ensure semantic parity — particularly
// for the "needs attention" status filter that includes devices with open actionable errors.
// If no pairing status filter is specified, defaults to PAIRED for backward compatibility.
func (s *SQLDeviceStore) GetDeviceIdentifiersByOrgWithFilter(ctx context.Context, orgID int64, filter *stores.MinerFilter) ([]string, error) {
	fp := buildMinerFilterParams(filter)

	// Default to PAIRED if no pairing status filter specified (backward compatibility)
	if !fp.pairingStatusFilter.Valid {
		fp.pairingStatusFilter = sql.NullString{Valid: true}
		fp.pairingStatusValues = []string{"PAIRED"}
	}

	var sb strings.Builder
	args := []any{orgID}
	argNum := 2

	sb.WriteString(`SELECT device.device_identifier
FROM device
JOIN discovered_device ON device.discovered_device_id = discovered_device.id
JOIN device_pairing ON device.id = device_pairing.device_id
LEFT JOIN device_status ON device.id = device_status.device_id
WHERE device.deleted_at IS NULL
    AND device.org_id = $1
    AND discovered_device.is_active = TRUE
    AND discovered_device.deleted_at IS NULL`)

	args, _ = appendFilterSQL(&sb, args, argNum, orgID, fp)

	rows, err := s.conn.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get filtered device identifiers for org %d: %v", orgID, err)
	}
	defer rows.Close()

	var identifiers []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to scan device identifier: %v", err)
		}
		identifiers = append(identifiers, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to iterate device identifiers: %v", err)
	}

	return identifiers, nil
}

// GetMinerStateCountsByCollections returns miner state counts grouped by collection ID.
// Bucket logic (offline/sleeping/broken/hashing) mirrors CountMinersByState in
// server/sqlc/queries/device.sql — see that query's header comment for priority rules.
func (s *SQLDeviceStore) GetMinerStateCountsByCollections(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64]stores.MinerStateCounts, error) {
	if len(collectionIDs) == 0 {
		return make(map[int64]stores.MinerStateCounts), nil
	}

	query := fmt.Sprintf(`SELECT dcm.device_set_id,
    -- Offline
    COALESCE(SUM(CASE
        WHEN ds.status = 'OFFLINE'
             OR (ds.status IS NULL AND dp.pairing_status != 'AUTHENTICATION_NEEDED')
        THEN 1 ELSE 0
    END), 0)::int AS offline_count,
    -- Sleeping
    COALESCE(SUM(CASE
        WHEN ds.status IN ('MAINTENANCE', 'INACTIVE')
             AND dp.pairing_status != 'AUTHENTICATION_NEEDED'
        THEN 1 ELSE 0
    END), 0)::int AS sleeping_count,
    -- Broken
    COALESCE(SUM(CASE
        WHEN ds.status IS DISTINCT FROM 'OFFLINE'
             AND NOT (ds.status IS NULL AND dp.pairing_status != 'AUTHENTICATION_NEEDED')
             AND NOT (ds.status IN ('MAINTENANCE', 'INACTIVE') AND dp.pairing_status != 'AUTHENTICATION_NEEDED')
             AND (ds.status IN ('ERROR', 'NEEDS_MINING_POOL', 'UPDATING', 'REBOOT_REQUIRED')
                  OR dp.pairing_status = 'AUTHENTICATION_NEEDED'
                  OR open_errors.device_id IS NOT NULL)
        THEN 1 ELSE 0
    END), 0)::int AS broken_count,
    -- Hashing
    COALESCE(SUM(CASE
        WHEN ds.status = 'ACTIVE'
             AND dp.pairing_status != 'AUTHENTICATION_NEEDED'
             AND open_errors.device_id IS NULL
        THEN 1 ELSE 0
    END), 0)::int AS hashing_count
FROM device_set_membership dcm
JOIN device_set dc ON dcm.device_set_id = dc.id
JOIN device d ON dcm.device_id = d.id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
-- Open actionable errors (severity 1-4; excludes UNSPECIFIED=0)
LEFT JOIN (
    SELECT DISTINCT device_id
    FROM errors
    WHERE errors.org_id = $1
      AND errors.closed_at IS NULL
      AND %s
) open_errors ON d.id = open_errors.device_id
WHERE dcm.device_set_id = ANY($2::bigint[])
  AND dcm.org_id = $1
  AND dc.deleted_at IS NULL
  AND d.deleted_at IS NULL
  AND dd.deleted_at IS NULL
  AND dd.is_active = TRUE
  AND dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
GROUP BY dcm.device_set_id`, actionableErrorSeveritiesExpr("errors"))

	rows, err := s.conn.QueryContext(ctx, query, orgID, pq.Array(collectionIDs))
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to count miner states by collections: %v", err)
	}
	defer rows.Close()

	result := make(map[int64]stores.MinerStateCounts)
	for rows.Next() {
		var collectionID int64
		var counts stores.MinerStateCounts
		if err := rows.Scan(&collectionID, &counts.OfflineCount, &counts.SleepingCount, &counts.BrokenCount, &counts.HashingCount); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to scan miner state counts: %v", err)
		}
		result[collectionID] = counts
	}
	if err := rows.Err(); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to iterate miner state counts: %v", err)
	}

	return result, nil
}

func (s *SQLDeviceStore) GetComponentErrorCountsByCollections(ctx context.Context, orgID int64, collectionIDs []int64) ([]stores.ComponentErrorCount, error) {
	if len(collectionIDs) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(`SELECT dcm.device_set_id, e.component_type, COUNT(DISTINCT e.device_id)::int AS device_count
FROM device_set_membership dcm
JOIN device_set dc ON dcm.device_set_id = dc.id AND dc.deleted_at IS NULL
JOIN device d ON dcm.device_id = d.id AND d.deleted_at IS NULL
JOIN discovered_device dd ON d.discovered_device_id = dd.id AND dd.is_active = TRUE
JOIN device_pairing dp ON d.id = dp.device_id
    AND %s
JOIN errors e ON d.id = e.device_id
    AND e.org_id = dcm.org_id
    AND e.closed_at IS NULL
    AND %s
    AND %s
WHERE dcm.device_set_id = ANY($2::bigint[]) AND dcm.org_id = $1
GROUP BY dcm.device_set_id, e.component_type`,
		actionablePairingStatusesExpr("dp"),
		actionableErrorSeveritiesExpr("e"),
		actionableErrorComponentTypesExpr("e"),
	)

	rows, err := s.conn.QueryContext(ctx, query, orgID, pq.Array(collectionIDs))
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get component error counts: %v", err)
	}
	defer rows.Close()

	var results []stores.ComponentErrorCount
	for rows.Next() {
		var r stores.ComponentErrorCount
		if err := rows.Scan(&r.CollectionID, &r.ComponentType, &r.DeviceCount); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to scan component error count: %v", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to iterate component error counts: %v", err)
	}
	return results, nil
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

func (s *SQLDeviceStore) UpdateWorkerName(ctx context.Context, deviceIdentifier models.DeviceIdentifier, workerName string) error {
	affected, err := s.getQueries(ctx).UpdateDeviceWorkerName(ctx, sqlc.UpdateDeviceWorkerNameParams{
		DeviceIdentifier: string(deviceIdentifier),
		WorkerName:       sql.NullString{String: workerName, Valid: workerName != ""},
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update worker name for device %s: %v", deviceIdentifier, err)
	}
	if affected == 0 {
		return fleeterror.NewNotFoundErrorf("device not found for worker name update with identifier=%s", deviceIdentifier)
	}
	return nil
}

// GetDevicePropertiesForRename fetches device attributes needed for name generation.
func (s *SQLDeviceStore) GetDevicePropertiesForRename(
	ctx context.Context,
	orgID int64,
	deviceIdentifiers []string,
	includeTelemetry bool,
) ([]stores.DeviceRenameProperties, error) {
	if len(deviceIdentifiers) == 0 {
		return nil, nil
	}

	if includeTelemetry {
		rows, err := s.getQueries(ctx).GetDevicePropertiesForRename(ctx, sqlc.GetDevicePropertiesForRenameParams{
			DeviceIdentifiers: deviceIdentifiers,
			OrgID:             orgID,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get device properties for rename: %v", err)
		}

		result := make([]stores.DeviceRenameProperties, 0, len(rows))
		for _, row := range rows {
			props := stores.DeviceRenameProperties{
				DeviceIdentifier:   row.DeviceIdentifier,
				DiscoveredDeviceID: row.DiscoveredDeviceID,
				CustomName:         row.CustomName,
				MacAddress:         row.MacAddress,
				IPAddress:          row.IpAddress,
			}
			if row.SerialNumber.Valid {
				props.SerialNumber = row.SerialNumber.String
			}
			if row.Model.Valid {
				props.Model = row.Model.String
				props.ModelSortValue = &props.Model
			}
			if row.Manufacturer.Valid {
				props.Manufacturer = row.Manufacturer.String
			}
			if row.FirmwareVersion.Valid {
				props.FirmwareVersion = row.FirmwareVersion.String
				props.FirmwareSortValue = &props.FirmwareVersion
			}
			if row.WorkerName.Valid {
				props.WorkerName = row.WorkerName.String
			}
			if row.WorkerNamePoolSyncStatus.Valid {
				props.WorkerNamePoolSyncStatus = string(row.WorkerNamePoolSyncStatus.WorkerNamePoolSyncStatusEnum)
			}
			if row.HashRateHs.Valid {
				hashrate := row.HashRateHs.Float64
				props.Hashrate = &hashrate
			}
			if row.TempC.Valid {
				temperature := row.TempC.Float64
				props.Temperature = &temperature
			}
			if row.PowerW.Valid {
				power := row.PowerW.Float64
				props.Power = &power
			}
			if row.EfficiencyJh.Valid {
				efficiency := row.EfficiencyJh.Float64
				props.Efficiency = &efficiency
			}
			result = append(result, props)
		}

		return result, nil
	}

	rows, err := s.getQueries(ctx).GetDevicePropertiesForRenameWithoutTelemetry(
		ctx,
		sqlc.GetDevicePropertiesForRenameWithoutTelemetryParams{
			DeviceIdentifiers: deviceIdentifiers,
			OrgID:             orgID,
		},
	)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get device properties for rename: %v", err)
	}

	result := make([]stores.DeviceRenameProperties, 0, len(rows))
	for _, row := range rows {
		props := stores.DeviceRenameProperties{
			DeviceIdentifier:   row.DeviceIdentifier,
			DiscoveredDeviceID: row.DiscoveredDeviceID,
			CustomName:         row.CustomName,
			MacAddress:         row.MacAddress,
			IPAddress:          row.IpAddress,
		}
		if row.SerialNumber.Valid {
			props.SerialNumber = row.SerialNumber.String
		}
		if row.Model.Valid {
			props.Model = row.Model.String
			props.ModelSortValue = &props.Model
		}
		if row.Manufacturer.Valid {
			props.Manufacturer = row.Manufacturer.String
		}
		if row.FirmwareVersion.Valid {
			props.FirmwareVersion = row.FirmwareVersion.String
			props.FirmwareSortValue = &props.FirmwareVersion
		}
		if row.WorkerName.Valid {
			props.WorkerName = row.WorkerName.String
		}
		if row.WorkerNamePoolSyncStatus.Valid {
			props.WorkerNamePoolSyncStatus = string(row.WorkerNamePoolSyncStatus.WorkerNamePoolSyncStatusEnum)
		}
		result = append(result, props)
	}

	return result, nil
}

// UpdateDeviceCustomNames sets the custom_name column on multiple devices atomically.
// The names map is keyed by device_identifier. Device ownership is validated by the
// caller (RenameMiners) before this method is invoked.
//
// The UPDATE and the row-count check run in a single transaction so that a short write
// (e.g. a concurrent soft-delete between selection and write) is rolled back rather than
// partially committed. This preserves all-or-nothing rename semantics.
func (s *SQLDeviceStore) UpdateDeviceCustomNames(ctx context.Context, orgID int64, names map[string]string) error {
	if len(names) == 0 {
		return nil
	}

	identifiers := make([]string, 0, len(names))
	customNames := make([]string, 0, len(names))
	for id, name := range names {
		identifiers = append(identifiers, id)
		customNames = append(customNames, name)
	}

	tx, err := s.conn.DB.BeginTx(ctx, nil)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to begin rename transaction: %v", err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx,
		`UPDATE device SET custom_name = updates.name
		FROM unnest($1::text[], $2::text[]) AS updates(identifier, name)
		WHERE device.device_identifier = updates.identifier
		  AND device.org_id = $3
		  AND device.deleted_at IS NULL`,
		pq.Array(identifiers), pq.Array(customNames), orgID,
	)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update device custom names: %v", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to read rows affected for custom name update: %v", err)
	}
	if int(affected) != len(names) {
		return fleeterror.NewNotFoundErrorf("one or more devices not found during rename: expected %d updates, got %d", len(names), affected)
	}

	if err = tx.Commit(); err != nil {
		return fleeterror.NewInternalErrorf("failed to commit rename transaction: %v", err)
	}

	return nil
}

func (s *SQLDeviceStore) GetPairedDeviceByMACAddress(ctx context.Context, macAddress string, orgID int64) (*stores.PairedDeviceInfo, error) {
	normalizedMAC := networking.NormalizeMAC(macAddress)
	if len(normalizedMAC) != 17 { // AA:BB:CC:DD:EE:FF
		return nil, fleeterror.NewNotFoundError(fmt.Sprintf("no paired device found with mac_address=%s org_id=%d", macAddress, orgID))
	}

	rows, err := s.getQueries(ctx).GetPairedDeviceByMACAddress(ctx, sqlc.GetPairedDeviceByMACAddressParams{
		NormalizedMac: normalizedMAC,
		OrgID:         orgID,
	})
	if err != nil {
		return nil, handleQueryError(err,
			fmt.Sprintf("no paired device found with mac_address=%s org_id=%d", normalizedMAC, orgID),
			fmt.Sprintf("failed to query paired device by MAC address=%s org_id=%d", normalizedMAC, orgID))
	}
	if len(rows) == 0 {
		return nil, fleeterror.NewNotFoundError(fmt.Sprintf("no paired device found with mac_address=%s org_id=%d", normalizedMAC, orgID))
	}
	if len(rows) > 1 {
		return nil, fleeterror.NewInternalErrorf("multiple paired devices found with mac_address=%s org_id=%d", normalizedMAC, orgID)
	}
	row := rows[0]

	return &stores.PairedDeviceInfo{
		DeviceIdentifier:           row.DeviceIdentifier,
		MacAddress:                 row.MacAddress,
		SerialNumber:               row.SerialNumber.String,
		DiscoveredDeviceIdentifier: row.DiscoveredDeviceIdentifier,
		DiscoveredDeviceID:         row.DiscoveredDeviceID,
	}, nil
}

func (s *SQLDeviceStore) GetPairedDevicesByMACAddresses(ctx context.Context, macAddresses []string, orgID int64) (map[string]*stores.PairedDeviceInfo, error) {
	if len(macAddresses) == 0 {
		return nil, nil
	}

	// Normalize all MACs before querying
	normalized := make([]string, 0, len(macAddresses))
	for _, mac := range macAddresses {
		n := networking.NormalizeMAC(mac)
		if len(n) == 17 { // AA:BB:CC:DD:EE:FF
			normalized = append(normalized, n)
		}
	}
	if len(normalized) == 0 {
		return nil, nil
	}

	rows, err := s.getQueries(ctx).GetPairedDevicesByMACAddresses(ctx, sqlc.GetPairedDevicesByMACAddressesParams{
		MacAddresses: normalized,
		OrgID:        orgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to batch query paired devices by MAC addresses: %v", err)
	}

	result := make(map[string]*stores.PairedDeviceInfo, len(rows))
	for _, row := range rows {
		result[row.MacAddress] = &stores.PairedDeviceInfo{
			DeviceIdentifier:           row.DeviceIdentifier,
			MacAddress:                 row.MacAddress,
			SerialNumber:               row.SerialNumber.String,
			DiscoveredDeviceIdentifier: row.DiscoveredDeviceIdentifier,
			DiscoveredDeviceID:         row.DiscoveredDeviceID,
		}
	}
	return result, nil
}

func (s *SQLDeviceStore) GetPairedDeviceBySerialNumber(ctx context.Context, serialNumber string, orgID int64) (*stores.PairedDeviceInfo, error) {
	row, err := s.getQueries(ctx).GetPairedDeviceBySerialNumber(ctx, sqlc.GetPairedDeviceBySerialNumberParams{
		SerialNumber: sql.NullString{String: serialNumber, Valid: serialNumber != ""},
		OrgID:        orgID,
	})
	if err != nil {
		return nil, handleQueryError(err,
			fmt.Sprintf("no paired device found with serial_number=%s org_id=%d", serialNumber, orgID),
			fmt.Sprintf("failed to query paired device by serial number=%s org_id=%d", serialNumber, orgID))
	}

	return &stores.PairedDeviceInfo{
		DeviceIdentifier:           row.DeviceIdentifier,
		MacAddress:                 row.MacAddress,
		SerialNumber:               row.SerialNumber.String,
		DiscoveredDeviceIdentifier: row.DiscoveredDeviceIdentifier,
		DiscoveredDeviceID:         row.DiscoveredDeviceID,
	}, nil
}
