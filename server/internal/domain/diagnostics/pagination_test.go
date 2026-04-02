package diagnostics

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
)

func TestEncodeCursor_WithValidData_ShouldReturnBase64Token(t *testing.T) {
	// Arrange
	severity := models.SeverityCritical
	lastSeenAt := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	errorID := "01HQXYZ123ABC"

	// Act
	token := EncodeCursor(severity, lastSeenAt, errorID)

	// Assert
	assert.NotEmpty(t, token)
	assert.NotContains(t, token, " ") // Should be URL-safe base64
}

func TestDecodeCursor_WithValidToken_ShouldReturnCursorData(t *testing.T) {
	// Arrange
	severity := models.SeverityCritical
	lastSeenAt := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	errorID := "01HQXYZ123ABC"
	token := EncodeCursor(severity, lastSeenAt, errorID)

	// Act
	cursor, err := DecodeCursor(token)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, cursor)
	assert.Equal(t, severity, cursor.Severity)
	assert.True(t, lastSeenAt.Equal(cursor.LastSeenAt))
	assert.Equal(t, errorID, cursor.ErrorID)
}

func TestDecodeCursor_WithEmptyToken_ShouldReturnNil(t *testing.T) {
	// Act
	cursor, err := DecodeCursor("")

	// Assert
	require.NoError(t, err)
	assert.Nil(t, cursor)
}

func TestDecodeCursor_WithInvalidBase64_ShouldReturnError(t *testing.T) {
	// Act
	cursor, err := DecodeCursor("not-valid-base64!!!")

	// Assert
	require.Error(t, err)
	assert.Nil(t, cursor)
	assert.Contains(t, err.Error(), "invalid cursor encoding")
}

func TestDecodeCursor_WithInvalidJSON_ShouldReturnError(t *testing.T) {
	// Arrange - valid base64 but invalid JSON
	token := "bm90LWpzb24=" // #nosec G101 -- test data, not credentials

	// Act
	cursor, err := DecodeCursor(token)

	// Assert
	require.Error(t, err)
	assert.Nil(t, cursor)
	assert.Contains(t, err.Error(), "invalid cursor format")
}

func TestEncodeDecode_RoundTrip_ShouldPreserveData(t *testing.T) {
	testCases := []struct {
		name       string
		severity   models.Severity
		lastSeenAt time.Time
		errorID    string
	}{
		{
			name:       "critical severity",
			severity:   models.SeverityCritical,
			lastSeenAt: time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
			errorID:    "01ABC123",
		},
		{
			name:       "info severity",
			severity:   models.SeverityInfo,
			lastSeenAt: time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC),
			errorID:    "01XYZ789",
		},
		{
			name:       "unspecified severity",
			severity:   models.SeverityUnspecified,
			lastSeenAt: time.Now().UTC().Truncate(time.Second),
			errorID:    "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			token := EncodeCursor(tc.severity, tc.lastSeenAt, tc.errorID)
			cursor, err := DecodeCursor(token)

			// Assert
			require.NoError(t, err)
			require.NotNil(t, cursor)
			assert.Equal(t, tc.severity, cursor.Severity)
			assert.True(t, tc.lastSeenAt.Equal(cursor.LastSeenAt))
			assert.Equal(t, tc.errorID, cursor.ErrorID)
		})
	}
}

func TestNormalizePageSize_WithValidSize_ShouldReturnSameSize(t *testing.T) {
	testCases := []struct {
		name     string
		input    int
		expected int
	}{
		{"minimum valid", 1, 1},
		{"mid range", 50, 50},
		{"default value", DefaultPageSize, DefaultPageSize},
		{"maximum valid", MaxPageSize, MaxPageSize},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := NormalizePageSize(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNormalizePageSize_WithInvalidSize_ShouldReturnDefault(t *testing.T) {
	testCases := []struct {
		name     string
		input    int
		expected int
	}{
		{"zero", 0, DefaultPageSize},
		{"negative", -1, DefaultPageSize},
		{"large negative", -100, DefaultPageSize},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := NormalizePageSize(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNormalizePageSize_WithOversizedValue_ShouldReturnMax(t *testing.T) {
	testCases := []struct {
		name     string
		input    int
		expected int
	}{
		{"just over max", MaxPageSize + 1, MaxPageSize},
		{"way over max", 10000, MaxPageSize},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := NormalizePageSize(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestBuildNextPageToken_WithFullPage_ShouldReturnToken(t *testing.T) {
	// Arrange
	now := time.Now()
	errors := make([]models.ErrorMessage, 50)
	for i := range errors {
		errors[i] = models.ErrorMessage{
			ErrorID:    fmt.Sprintf("ERROR_%d", i),
			Severity:   models.SeverityMajor,
			LastSeenAt: now.Add(time.Duration(-i) * time.Minute),
		}
	}
	pageSize := 50

	// Act
	token := BuildNextPageToken(errors, pageSize)

	// Assert
	assert.NotEmpty(t, token)

	// Verify the token contains the last error's data
	cursor, err := DecodeCursor(token)
	require.NoError(t, err)
	lastError := errors[len(errors)-1]
	assert.Equal(t, lastError.Severity, cursor.Severity)
	assert.Equal(t, lastError.ErrorID, cursor.ErrorID)
}

func TestBuildNextPageToken_WithPartialPage_ShouldReturnEmptyString(t *testing.T) {
	// Arrange - less errors than page size indicates last page
	now := time.Now()
	errors := []models.ErrorMessage{
		{ErrorID: "ERR1", Severity: models.SeverityMajor, LastSeenAt: now},
		{ErrorID: "ERR2", Severity: models.SeverityMinor, LastSeenAt: now},
	}
	pageSize := 50

	// Act
	token := BuildNextPageToken(errors, pageSize)

	// Assert
	assert.Empty(t, token)
}

func TestBuildNextPageToken_WithEmptySlice_ShouldReturnEmptyString(t *testing.T) {
	// Act
	token := BuildNextPageToken([]models.ErrorMessage{}, 50)

	// Assert
	assert.Empty(t, token)
}

func TestBuildNextPageToken_WithNilSlice_ShouldReturnEmptyString(t *testing.T) {
	// Act
	token := BuildNextPageToken(nil, 50)

	// Assert
	assert.Empty(t, token)
}
