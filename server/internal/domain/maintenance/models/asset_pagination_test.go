package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTicketAssetPaginationDefaultsAndBounds(t *testing.T) {
	size, cursor, err := ParseTicketAssetPagination(0, "", "q")
	require.NoError(t, err)
	require.Equal(t, int32(50), size)
	require.Zero(t, cursor.ID)

	_, _, err = ParseTicketAssetPagination(201, "", "q")
	require.Error(t, err)
	_, _, err = ParseTicketAssetPagination(-1, "", "q")
	require.Error(t, err)
}

func TestTicketAssetCursorRoundTripsForTheSameQuery(t *testing.T) {
	query := TicketAssetQuery{Scope: "options", OrgID: 1, Search: "a"}.Fingerprint()
	page, err := TicketAssetPageFromRows([]TicketAsset{{Kind: TicketCategoryMiner, ID: 3}, {Kind: TicketCategoryModule, ID: 4}}, 1, query)
	require.NoError(t, err)
	require.Len(t, page.Assets, 1)
	require.NotEmpty(t, page.NextPageToken)

	_, cursor, err := ParseTicketAssetPagination(1, page.NextPageToken, query)
	require.NoError(t, err)
	require.Equal(t, TicketAssetCursor{Kind: TicketCategoryMiner, ID: 3, Fingerprint: query}, cursor)
}

func TestTicketAssetCursorRejectsAChangedQuery(t *testing.T) {
	base := TicketAssetQuery{Scope: "options", OrgID: 1, SiteIDs: []int64{1, 2}, ExcludedSiteIDs: []int64{9}, Kind: TicketCategoryMiner, Search: "a"}
	page, err := TicketAssetPageFromRows([]TicketAsset{{Kind: TicketCategoryMiner, ID: 3}, {Kind: TicketCategoryMiner, ID: 4}}, 1, base.Fingerprint())
	require.NoError(t, err)

	reordered := base
	reordered.SiteIDs = []int64{2, 1, 2}
	_, _, err = ParseTicketAssetPagination(1, page.NextPageToken, reordered.Fingerprint())
	require.NoError(t, err, "equivalent site sets describe the same query")

	for name, changed := range map[string]func(*TicketAssetQuery){
		"search":     func(q *TicketAssetQuery) { q.Search = "b" },
		"org":        func(q *TicketAssetQuery) { q.OrgID = 2 },
		"sites":      func(q *TicketAssetQuery) { q.SiteIDs = []int64{1} },
		"excluded":   func(q *TicketAssetQuery) { q.ExcludedSiteIDs = nil },
		"kind":       func(q *TicketAssetQuery) { q.Kind = TicketCategoryModule },
		"unassigned": func(q *TicketAssetQuery) { q.IncludeUnassigned = true },
		"ticket":     func(q *TicketAssetQuery) { q.TicketID = 5 },
		"account":    func(q *TicketAssetQuery) { q.AccountID = 5 },
		"scope":      func(q *TicketAssetQuery) { q.Scope = "board-options" },
	} {
		t.Run(name, func(t *testing.T) {
			query := base
			changed(&query)
			_, _, err := ParseTicketAssetPagination(1, page.NextPageToken, query.Fingerprint())
			require.ErrorContains(t, err, "page token does not match this query")
		})
	}
}

func TestTicketAssetCursorRejectsMalformedTokens(t *testing.T) {
	for _, token := range []string{"!", "e30", "eyJraW5kIjo5OSwiaWQiOjF9"} {
		_, _, err := ParseTicketAssetPagination(1, token, "q")
		require.ErrorContains(t, err, "invalid asset page token", token)
	}
}
