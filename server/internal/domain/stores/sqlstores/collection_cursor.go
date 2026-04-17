package sqlstores

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// collectionCursor holds pagination state for ListCollections.
// SortField and SortDir track the ordering used when the cursor was created so
// stale cursors from a different sort configuration are rejected.
type collectionCursor struct {
	Label       string  `json:"l"`
	ID          int64   `json:"id"`
	SortField   string  `json:"sf,omitempty"`
	SortDir     string  `json:"sd,omitempty"`
	DeviceCount *int32  `json:"dc,omitempty"`
	IssueCount  *int32  `json:"ic,omitempty"`
	Zone        *string `json:"z,omitempty"`
}

// memberCursor holds pagination state for ListCollectionMembers (ordered by created_at DESC, id DESC).
type memberCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        int64     `json:"id"`
}

func encodeCollectionCursor(c *collectionCursor) string {
	if c == nil {
		return ""
	}
	data, err := json.Marshal(c)
	if err != nil {
		slog.Error("failed to encode collection cursor", "error", err, "cursor_id", c.ID)
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func decodeCollectionCursor(encoded string) (*collectionCursor, error) {
	if encoded == "" {
		return nil, nil
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid cursor encoding: %v", err)
	}

	var cursor collectionCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid cursor format: %v", err)
	}

	return &cursor, nil
}

func encodeMemberCursor(c *memberCursor) string {
	if c == nil {
		return ""
	}
	data, err := json.Marshal(c)
	if err != nil {
		slog.Error("failed to encode member cursor", "error", err, "cursor_id", c.ID)
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func decodeMemberCursor(encoded string) (*memberCursor, error) {
	if encoded == "" {
		return nil, nil
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid cursor encoding: %v", err)
	}

	var cursor memberCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid cursor format: %v", err)
	}

	return &cursor, nil
}
