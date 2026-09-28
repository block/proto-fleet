package rollout

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// MinerFirmwareHistoryEntry is one saved rollout target for a paired miner.
// CreatedAt and FinishedAt belong to the rollout; progress belongs to the miner.
type MinerFirmwareHistoryEntry struct {
	RolloutID        int64
	ChannelID        int64
	ChannelName      string
	Manufacturer     string
	Model            string
	FirmwareVersion  string
	FirmwareChecksum string
	RolloutStatus    string
	CancelReason     string
	Paused           bool
	Phase            string
	Attempts         int32
	LastError        string
	SkipNote         string
	CreatedAt        time.Time
	FinishedAt       *time.Time
	LastSentAt       *time.Time
	VerifiedAt       *time.Time
}

const minerHistoryCursorVersion = "miner-history-v1"
const maxMinerHistoryCursorLength = 256

// ListMinerFirmwareHistory pages a current paired miner's retained rollout
// history by descending (created_at, id), independently of current membership.
func (s *Service) ListMinerFirmwareHistory(ctx context.Context, orgID int64, deviceIdentifier string, pageSize int32, cursor string) ([]MinerFirmwareHistoryEntry, string, error) {
	if !utf8.ValidString(deviceIdentifier) || deviceIdentifier == "" || utf8.RuneCountInString(deviceIdentifier) > 255 || strings.ContainsRune(deviceIdentifier, '\x00') {
		return nil, "", fleeterror.NewInvalidArgumentError("invalid device_identifier")
	}
	if pageSize < 0 || pageSize > MaxPageSize {
		return nil, "", fleeterror.NewInvalidArgumentError("page_size must be between 0 and 1000")
	}
	if len(cursor) > maxMinerHistoryCursorLength {
		return nil, "", fleeterror.NewInvalidArgumentError("invalid cursor")
	}
	q := s.store.GetQueries(ctx)
	deviceID, err := q.GetMinerFirmwareHistoryDeviceID(ctx, sqlc.GetMinerFirmwareHistoryDeviceIDParams{OrgID: orgID, DeviceIdentifier: deviceIdentifier})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", fleeterror.NewNotFoundError("miner not found")
	}
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("get miner for firmware history: %w", err)
	}
	limit := clampPageSize(pageSize)
	params := sqlc.ListMinerFirmwareHistoryParams{OrgID: orgID, DeviceID: deviceID, PageLimit: limit + 1}
	if cursor != "" {
		parts, err := decodeCursor(cursor, 5)
		if err != nil {
			return nil, "", err
		}
		if parts[0] != minerHistoryCursorVersion || parts[1] != strconv.FormatInt(orgID, 10) || parts[2] != strconv.FormatInt(deviceID, 10) {
			return nil, "", fleeterror.NewInvalidArgumentError("cursor does not match miner history")
		}
		micros, timeErr := strconv.ParseInt(parts[3], 10, 64)
		id, idErr := strconv.ParseInt(parts[4], 10, 64)
		if timeErr != nil || idErr != nil || id <= 0 {
			return nil, "", fleeterror.NewInvalidArgumentError("invalid cursor")
		}
		createdAt := time.UnixMicro(micros).UTC()
		if createdAt.Year() < 1 || createdAt.Year() > 9999 {
			return nil, "", fleeterror.NewInvalidArgumentError("invalid cursor")
		}
		params.BeforeCreatedAt = sql.NullTime{Time: createdAt, Valid: true}
		params.BeforeID = sql.NullInt64{Int64: id, Valid: true}
	}
	rows, err := q.ListMinerFirmwareHistory(ctx, params)
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("list miner firmware history: %w", err)
	}
	next := ""
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeCursor(minerHistoryCursorVersion, strconv.FormatInt(orgID, 10), strconv.FormatInt(deviceID, 10), strconv.FormatInt(last.CreatedAt.UnixMicro(), 10), strconv.FormatInt(last.RolloutID, 10))
	}
	entries := make([]MinerFirmwareHistoryEntry, 0, len(rows))
	for _, row := range rows {
		entry := MinerFirmwareHistoryEntry{
			RolloutID: row.RolloutID, ChannelID: row.ChannelID, ChannelName: row.ChannelName,
			Manufacturer: row.Manufacturer, Model: row.Model,
			FirmwareVersion: row.FirmwareVersion, FirmwareChecksum: row.FirmwareChecksum,
			RolloutStatus: row.RolloutStatus, CancelReason: row.CancelReason,
			Paused: row.RolloutStatus == StatusActive && row.PausedAt.Valid,
			Phase: persistedDevicePhase(row.RolloutStatus, persistedPhase{
				Excluded: row.ExcludedAt.Valid, Halted: row.HaltedAt.Valid, Verified: row.VerifiedAt.Valid,
				HaltReason: row.HaltReason, Attempts: row.Attempts,
			}),
			Attempts: row.Attempts, LastError: row.LastError, SkipNote: row.SkipNote,
			CreatedAt: row.CreatedAt,
		}
		if row.FinishedAt.Valid {
			entry.FinishedAt = &row.FinishedAt.Time
		}
		if row.LastSentAt.Valid {
			entry.LastSentAt = &row.LastSentAt.Time
		}
		if row.VerifiedAt.Valid {
			entry.VerifiedAt = &row.VerifiedAt.Time
		}
		entries = append(entries, entry)
	}
	return entries, next, nil
}
