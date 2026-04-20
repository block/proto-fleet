package sqlstores

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeSortedCursor(t *testing.T) {
	cursor := &sortedCursor{
		SortField:     stores.SortFieldName,
		SortDirection: stores.SortDirectionAsc,
		SortValue:     "Bitmain S19",
		CursorID:      42,
	}

	result := encodeSortedCursor(cursor)

	require.NotEmpty(t, result)
	decoded, err := base64.StdEncoding.DecodeString(result)
	require.NoError(t, err)
	var decodedCursor sortedCursor
	require.NoError(t, json.Unmarshal(decoded, &decodedCursor))
	assert.Equal(t, cursor.SortField, decodedCursor.SortField)
	assert.Equal(t, cursor.SortDirection, decodedCursor.SortDirection)
	assert.Equal(t, cursor.SortValue, decodedCursor.SortValue)
	assert.Equal(t, cursor.CursorID, decodedCursor.CursorID)
}

func TestDecodeSortedCursor_ConfigMismatchRejected(t *testing.T) {
	// Tests business logic: changing sort config between pages is rejected
	cursorData := sortedCursor{
		SortField:     stores.SortFieldName,
		SortDirection: stores.SortDirectionAsc,
		SortValue:     "test",
		CursorID:      1,
	}
	data, err := json.Marshal(cursorData)
	require.NoError(t, err)
	encoded := base64.StdEncoding.EncodeToString(data)

	// Request with different sort config
	sortConfig := &stores.SortConfig{
		Field:     stores.SortFieldIPAddress,
		Direction: stores.SortDirectionDesc,
	}

	cursor, err := decodeSortedCursor(encoded, sortConfig)

	require.Error(t, err)
	assert.Nil(t, cursor)
	assert.Contains(t, err.Error(), "cursor sort config mismatch")
}

func TestDecodeSortedCursor_MatchingConfig(t *testing.T) {
	cursorData := sortedCursor{
		SortField:     stores.SortFieldName,
		SortDirection: stores.SortDirectionAsc,
		SortValue:     "test value",
		CursorID:      123,
	}
	data, marshalErr := json.Marshal(cursorData)
	require.NoError(t, marshalErr)
	encoded := base64.StdEncoding.EncodeToString(data)
	sortConfig := &stores.SortConfig{
		Field:     stores.SortFieldName,
		Direction: stores.SortDirectionAsc,
	}

	cursor, err := decodeSortedCursor(encoded, sortConfig)

	require.NoError(t, err)
	require.NotNil(t, cursor)
	assert.Equal(t, stores.SortFieldName, cursor.SortField)
	assert.Equal(t, stores.SortDirectionAsc, cursor.SortDirection)
	assert.Equal(t, "test value", cursor.SortValue)
	assert.Equal(t, int64(123), cursor.CursorID)
}

func TestRoundTripCursor(t *testing.T) {
	original := &sortedCursor{
		SortField:     stores.SortFieldHashrate,
		SortDirection: stores.SortDirectionDesc,
		SortValue:     "123.456",
		CursorID:      789,
	}
	sortConfig := &stores.SortConfig{
		Field:     stores.SortFieldHashrate,
		Direction: stores.SortDirectionDesc,
	}

	encoded := encodeSortedCursor(original)
	decoded, err := decodeSortedCursor(encoded, sortConfig)

	require.NoError(t, err)
	require.NotNil(t, decoded)
	assert.Equal(t, original.SortField, decoded.SortField)
	assert.Equal(t, original.SortDirection, decoded.SortDirection)
	assert.Equal(t, original.SortValue, decoded.SortValue)
	assert.Equal(t, original.CursorID, decoded.CursorID)
}

// TestCursorSortConfigMismatch_AllCombinations tests that changing sort config between pages is rejected.
func TestCursorSortConfigMismatch_AllCombinations(t *testing.T) {
	tests := []struct {
		name           string
		cursorField    stores.SortField
		cursorDir      stores.SortDirection
		requestField   stores.SortField
		requestDir     stores.SortDirection
		expectError    bool
		errorSubstring string
	}{
		{
			name:         "same field and direction succeeds",
			cursorField:  stores.SortFieldName,
			cursorDir:    stores.SortDirectionAsc,
			requestField: stores.SortFieldName,
			requestDir:   stores.SortDirectionAsc,
			expectError:  false,
		},
		{
			name:           "different field rejected",
			cursorField:    stores.SortFieldName,
			cursorDir:      stores.SortDirectionAsc,
			requestField:   stores.SortFieldIPAddress,
			requestDir:     stores.SortDirectionAsc,
			expectError:    true,
			errorSubstring: "cursor sort config mismatch",
		},
		{
			name:           "different direction rejected",
			cursorField:    stores.SortFieldName,
			cursorDir:      stores.SortDirectionAsc,
			requestField:   stores.SortFieldName,
			requestDir:     stores.SortDirectionDesc,
			expectError:    true,
			errorSubstring: "cursor sort config mismatch",
		},
		{
			name:           "unspecified cursor with specified request rejected",
			cursorField:    stores.SortFieldUnspecified,
			cursorDir:      stores.SortDirectionUnspecified,
			requestField:   stores.SortFieldName,
			requestDir:     stores.SortDirectionAsc,
			expectError:    true,
			errorSubstring: "cursor sort config mismatch",
		},
		{
			name:           "specified cursor with unspecified request rejected",
			cursorField:    stores.SortFieldName,
			cursorDir:      stores.SortDirectionAsc,
			requestField:   stores.SortFieldUnspecified,
			requestDir:     stores.SortDirectionUnspecified,
			expectError:    true,
			errorSubstring: "cursor sort config mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cursorData := sortedCursor{
				SortField:     tt.cursorField,
				SortDirection: tt.cursorDir,
				SortValue:     "test-value",
				CursorID:      100,
			}
			data, _ := json.Marshal(cursorData)
			encoded := base64.StdEncoding.EncodeToString(data)

			var requestConfig *stores.SortConfig
			if tt.requestField != stores.SortFieldUnspecified || tt.requestDir != stores.SortDirectionUnspecified {
				requestConfig = &stores.SortConfig{
					Field:     tt.requestField,
					Direction: tt.requestDir,
				}
			}

			cursor, err := decodeSortedCursor(encoded, requestConfig)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, cursor)
				assert.Contains(t, err.Error(), tt.errorSubstring)
			} else {
				require.NoError(t, err)
				require.NotNil(t, cursor)
			}
		})
	}
}

func TestCursorWithEmptySortValue(t *testing.T) {
	// Tests cursors from rows with NULL telemetry values
	original := &sortedCursor{
		SortField:     stores.SortFieldHashrate,
		SortDirection: stores.SortDirectionDesc,
		SortValue:     "", // NULL/empty
		CursorID:      999,
	}
	config := &stores.SortConfig{
		Field:     stores.SortFieldHashrate,
		Direction: stores.SortDirectionDesc,
	}

	encoded := encodeSortedCursor(original)
	decoded, err := decodeSortedCursor(encoded, config)

	require.NoError(t, err)
	require.NotNil(t, decoded)
	assert.Equal(t, "", decoded.SortValue)
	assert.Equal(t, int64(999), decoded.CursorID)
}
