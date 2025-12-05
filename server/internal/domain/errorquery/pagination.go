package errorquery

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
)

// Pagination constants.
const (
	DefaultPageSize = 50
	MaxPageSize     = 1000
	MinPageSize     = 1
)

// PageCursor holds pagination state.
type PageCursor struct {
	Offset int                 `json:"offset"`
	View   errorsv1.ResultView `json:"view"`
}

// EncodePageToken encodes a cursor into a base64 page token.
func EncodePageToken(cursor PageCursor) string {
	data, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(data)
}

// DecodePageToken decodes a base64 page token into a cursor.
func DecodePageToken(token string) (PageCursor, error) {
	if token == "" {
		return PageCursor{Offset: 0}, nil
	}

	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return PageCursor{}, fmt.Errorf("invalid page token: %w", err)
	}

	var cursor PageCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return PageCursor{}, fmt.Errorf("invalid page token format: %w", err)
	}

	if cursor.Offset < 0 {
		return PageCursor{}, fmt.Errorf("invalid page token: negative offset")
	}

	return cursor, nil
}

// NormalizePageSize ensures page size is within valid bounds.
func NormalizePageSize(pageSize int32) int {
	if pageSize <= 0 {
		return DefaultPageSize
	}
	if pageSize > MaxPageSize {
		return MaxPageSize
	}
	return int(pageSize)
}

// Paginate returns a slice of items for the current page and the next page token.
func Paginate[T any](items []T, offset, pageSize int, view errorsv1.ResultView) ([]T, string, int64) {
	totalCount := int64(len(items))

	if offset >= len(items) {
		return []T{}, "", totalCount
	}

	end := offset + pageSize
	if end > len(items) {
		end = len(items)
	}

	page := items[offset:end]

	var nextToken string
	if end < len(items) {
		nextToken = EncodePageToken(PageCursor{
			Offset: end,
			View:   view,
		})
	}

	return page, nextToken, totalCount
}
