package sqlstores

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"slices"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// curtailmentEventCursor carries pagination state for ListCurtailmentEvents.
// It is bound to the org and state filter that issued it so callers cannot
// silently skip rows by reusing a token across different list queries.
type curtailmentEventCursor struct {
	ID int64 `json:"id"`

	OrgID int64 `json:"org_id"`

	// StateFilter is retained for pre-state_filters page tokens. New tokens
	// use StateFilters so a cursor is bound to the complete filter set.
	StateFilter  models.EventState   `json:"state_filter,omitempty"`
	StateFilters []models.EventState `json:"state_filters,omitempty"`
}

func normalizeCurtailmentEventStateFilters(filters []models.EventState) []models.EventState {
	out := make([]models.EventState, 0, len(filters))
	for _, filter := range filters {
		if filter != "" && !slices.Contains(out, filter) {
			out = append(out, filter)
		}
	}
	return out
}

func curtailmentEventStateFiltersEqual(left, right []models.EventState) bool {
	if len(left) != len(right) {
		return false
	}
	for _, filter := range left {
		if !slices.Contains(right, filter) {
			return false
		}
	}
	return true
}

func encodeCurtailmentEventCursor(c *curtailmentEventCursor) string {
	if c == nil {
		return ""
	}
	data, err := json.Marshal(c)
	if err != nil {
		slog.Error("failed to encode curtailment event cursor", "error", err, "cursor_id", c.ID, "org_id", c.OrgID)
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func decodeCurtailmentEventCursor(encoded string) (*curtailmentEventCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid page_token encoding: %v", err)
	}
	var cursor curtailmentEventCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid page_token format: %v", err)
	}
	if cursor.ID <= 0 {
		// Defense against user-supplied tokens that decode to zero/negative.
		// The store never emits a non-positive id; a non-positive cursor
		// would either rewind to the first page (id=0) or return zero rows
		// (id<0), both of which look like a silent client bug.
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid page_token: id must be > 0, got %d", cursor.ID)
	}
	if cursor.OrgID < 0 {
		// Explicit negative org_id is tampering; reject loudly.
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid page_token: org_id must be >= 0, got %d", cursor.OrgID)
	}
	if cursor.OrgID == 0 {
		// Legacy token: pre-org-binding cursors omitted org_id and decode
		// to the JSON zero value. Restart from the first page so an old
		// client's in-flight pagination loop continues from the top
		// instead of surfacing InvalidArgument on the next page request.
		return nil, nil
	}
	if len(cursor.StateFilters) == 0 && cursor.StateFilter != "" {
		cursor.StateFilters = []models.EventState{cursor.StateFilter}
	}
	cursor.StateFilters = normalizeCurtailmentEventStateFilters(cursor.StateFilters)
	return &cursor, nil
}

// curtailmentTargetCursor carries pagination state for targets inside one
// event. It is bound to org/event so a token cannot silently skip targets when
// reused against a different expanded activity.
type curtailmentTargetCursor struct {
	OrgID            int64     `json:"org_id"`
	EventUUID        uuid.UUID `json:"event_uuid"`
	DeviceIdentifier string    `json:"device_identifier"`
}

func encodeCurtailmentTargetCursor(c *curtailmentTargetCursor) string {
	if c == nil {
		return ""
	}
	data, err := json.Marshal(c)
	if err != nil {
		slog.Error("failed to encode curtailment target cursor",
			"error", err, "org_id", c.OrgID, "event_uuid", c.EventUUID)
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func decodeCurtailmentTargetCursor(encoded string) (*curtailmentTargetCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid target_page_token encoding: %v", err)
	}
	var cursor curtailmentTargetCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid target_page_token format: %v", err)
	}
	if cursor.OrgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid target_page_token: org_id must be > 0, got %d", cursor.OrgID)
	}
	if cursor.EventUUID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid target_page_token: event_uuid must be set")
	}
	if cursor.DeviceIdentifier == "" {
		return nil, fleeterror.NewInvalidArgumentError("invalid target_page_token: device_identifier must be set")
	}
	return &cursor, nil
}
