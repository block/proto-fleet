package sqlstores

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// curtailmentEventCursor carries pagination state for ListCurtailmentEvents.
// Ordering is fixed (id DESC), so the cursor only needs the last id seen;
// future cursor fields (e.g. SortField when alternative orderings ship)
// extend the struct without breaking older clients because JSON-encoded
// fields are tolerated on decode.
type curtailmentEventCursor struct {
	ID int64 `json:"id"`
}

func encodeCurtailmentEventCursor(c *curtailmentEventCursor) string {
	if c == nil {
		return ""
	}
	data, err := json.Marshal(c)
	if err != nil {
		slog.Error("failed to encode curtailment event cursor", "error", err, "cursor_id", c.ID)
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
	return &cursor, nil
}
