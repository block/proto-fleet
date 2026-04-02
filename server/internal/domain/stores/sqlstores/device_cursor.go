package sqlstores

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// sortedCursor holds pagination state for sorted queries.
type sortedCursor struct {
	SortField     stores.SortField     `json:"f"`
	SortDirection stores.SortDirection `json:"d"`
	SortValue     string               `json:"v"`
	CursorID      int64                `json:"id"`
}

func encodeSortedCursor(c *sortedCursor) string {
	if c == nil {
		return ""
	}
	data, err := json.Marshal(c)
	if err != nil {
		slog.Error("failed to encode cursor", "error", err, "cursor_id", c.CursorID)
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func decodeSortedCursor(encoded string, sortConfig *stores.SortConfig) (*sortedCursor, error) {
	if encoded == "" {
		return nil, nil
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid cursor encoding: %v", err)
	}

	var cursor sortedCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid cursor format: %v", err)
	}

	expectedField := stores.SortFieldUnspecified
	expectedDirection := stores.SortDirectionUnspecified
	if sortConfig != nil {
		expectedField = sortConfig.Field
		expectedDirection = sortConfig.Direction
	}

	if cursor.SortField != expectedField || cursor.SortDirection != expectedDirection {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"cursor sort config mismatch: cursor has field=%d,dir=%d but request has field=%d,dir=%d; reset to first page when changing sort",
			cursor.SortField, cursor.SortDirection, expectedField, expectedDirection)
	}

	return &cursor, nil
}
