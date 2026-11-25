package models

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type TicketAssetOptionsParams struct {
	OrgID           int64
	TicketID        int64
	SiteIDs         []int64
	ExcludedSiteIDs []int64
	Kind            TicketCategory
	Search          string
	// IncludeUnassigned also returns assets with no site alongside SiteIDs. Callers
	// must set it only when the actor's read scope is org-wide.
	IncludeUnassigned bool
	PageSize          int32
	PageToken         string
}

type TicketAssetPage struct {
	Assets        []TicketAsset
	NextPageToken string
}

// TicketAssetQuery is the effective filter set behind one asset listing. Its
// fingerprint binds a page token to the query that issued it.
type TicketAssetQuery struct {
	Scope             string
	OrgID             int64
	TicketID          int64
	AccountID         int64
	SiteIDs           []int64
	ExcludedSiteIDs   []int64
	Kind              TicketCategory
	Search            string
	IncludeUnassigned bool
}

func (q TicketAssetQuery) Fingerprint() string {
	canonical := func(ids []int64) []int64 {
		out := slices.Clone(ids)
		slices.Sort(out)
		return slices.Compact(out)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%q\x00%d\x00%d\x00%d\x00%v\x00%v\x00%d\x00%q\x00%t",
		q.Scope, q.OrgID, q.TicketID, q.AccountID, canonical(q.SiteIDs), canonical(q.ExcludedSiteIDs), q.Kind, q.Search, q.IncludeUnassigned)))
	return hex.EncodeToString(sum[:])
}

type TicketAssetCursor struct {
	Kind        TicketCategory `json:"kind"`
	ID          int64          `json:"id"`
	Fingerprint string         `json:"query"`
}

// ParseTicketAssetPagination validates the page size and, for a continuation, the
// token, which must have been issued for a query with the same fingerprint.
func ParseTicketAssetPagination(size int32, token, fingerprint string) (int32, TicketAssetCursor, error) {
	var cursor TicketAssetCursor
	if size < 0 || size > 200 {
		return 0, cursor, fleeterror.NewInvalidArgumentError("asset page size must be between 0 and 200")
	}
	if size == 0 {
		size = 50
	}
	if token != "" {
		data, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.ID <= 0 {
			return 0, cursor, fleeterror.NewInvalidArgumentError("invalid asset page token")
		}
		if _, valid := PhysicalTicketCategory(int32(cursor.Kind)); !valid {
			return 0, cursor, fleeterror.NewInvalidArgumentError("invalid asset page token")
		}
		if cursor.Fingerprint != fingerprint {
			return 0, TicketAssetCursor{}, fleeterror.NewInvalidArgumentError("page token does not match this query")
		}
	}
	return size, cursor, nil
}

func (c TicketAssetCursor) Encode() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("encode asset page cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func TicketAssetPageFromRows(assets []TicketAsset, size int32, fingerprint string) (*TicketAssetPage, error) {
	page := &TicketAssetPage{Assets: assets}
	if len(assets) > int(size) {
		page.Assets = assets[:size]
		last := page.Assets[len(page.Assets)-1]
		var err error
		page.NextPageToken, err = (TicketAssetCursor{Kind: last.Kind, ID: last.ID, Fingerprint: fingerprint}).Encode()
		if err != nil {
			return nil, err
		}
	}
	return page, nil
}
