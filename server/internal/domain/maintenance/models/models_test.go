package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTicketCursorRoundTripIsOpaqueAndLossless(t *testing.T) {
	want := TicketCursor{
		SortField:     TicketSortFieldLocation,
		SortDirection: SortDirectionAscending,
		Value:         "mine one / building a",
		ID:            42,
	}
	token, err := want.Encode()
	require.NoError(t, err)
	assert.NotContains(t, token, want.Value)

	got, err := DecodeTicketCursor(token)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestDecodeTicketCursorRejectsMalformedOrIncompleteTokens(t *testing.T) {
	for _, token := range []string{"not-base64!", "e30", string(make([]byte, 2049))} { // e30 is {}
		_, err := DecodeTicketCursor(token)
		assert.Error(t, err)
	}
}
