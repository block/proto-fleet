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

// buildingRackCursor holds pagination state for ListBuildingRacks
// (ordered by (ds.label, dsr.device_set_id) ASC). ID breaks label
// ties so the cursor is unique across the result set.
type buildingRackCursor struct {
	Label string `json:"l"`
	ID    int64  `json:"id"`
}

// encodeCursor / decodeCursor are the shared base64-JSON cursor
// codec all paginated list stores use. The generic param makes
// each call site self-documenting about the cursor shape it's
// emitting. errLabel feeds the cursor-encode error log so a failed
// encode names the offending list (rather than collapsing into a
// generic "cursor encode failed" message).
func encodeCursor[T any](c *T, errLabel string) string {
	if c == nil {
		return ""
	}
	data, err := json.Marshal(c)
	if err != nil {
		slog.Error("failed to encode cursor", "error", err, "cursor_type", errLabel)
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func decodeCursor[T any](encoded string) (*T, error) {
	if encoded == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid cursor encoding: %v", err)
	}
	var cursor T
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid cursor format: %v", err)
	}
	return &cursor, nil
}

// Thin wrappers preserve the named call-site signatures so the
// existing store paths read clearly at a glance.
func encodeCollectionCursor(c *collectionCursor) string {
	return encodeCursor(c, "collection")
}

func decodeCollectionCursor(encoded string) (*collectionCursor, error) {
	return decodeCursor[collectionCursor](encoded)
}

func encodeMemberCursor(c *memberCursor) string {
	return encodeCursor(c, "member")
}

func decodeMemberCursor(encoded string) (*memberCursor, error) {
	return decodeCursor[memberCursor](encoded)
}

func encodeBuildingRackCursor(c *buildingRackCursor) string {
	return encodeCursor(c, "building-rack")
}

func decodeBuildingRackCursor(encoded string) (*buildingRackCursor, error) {
	return decodeCursor[buildingRackCursor](encoded)
}
