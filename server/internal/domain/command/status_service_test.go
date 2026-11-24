package command

import (
	"encoding/json"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDeviceCount_BothSuccessAndFailureIdentifiers(t *testing.T) {
	t.Run("parses both success and failure device identifiers", func(t *testing.T) {
		// Arrange
		successIDs := []string{"device1", "device2"}
		failureIDs := []string{"device3", "device4", "device5"}

		successJSON, err := json.Marshal(successIDs)
		require.NoError(t, err)

		failureJSON, err := json.Marshal(failureIDs)
		require.NoError(t, err)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             5,
			SuccessfulDevices:        2,
			FailedDevices:            3,
			SuccessDeviceIdentifiers: successJSON,
			FailureDeviceIdentifiers: failureJSON,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Equal(t, int64(5), result.Total)
		assert.Equal(t, int64(2), result.Success)
		assert.Equal(t, int64(3), result.Failure)
		assert.Equal(t, successIDs, result.SuccessDeviceIdentifiers)
		assert.Equal(t, failureIDs, result.FailureDeviceIdentifiers)
	})
}

func TestGetDeviceCount_FilterNullValues(t *testing.T) {
	t.Run("returns empty slice when all values are empty strings", func(t *testing.T) {
		// Arrange
		allNulls := []string{"", "", ""}
		jsonBytes, err := json.Marshal(allNulls)
		require.NoError(t, err)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             0,
			SuccessfulDevices:        0,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: jsonBytes,
			FailureDeviceIdentifiers: jsonBytes,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Empty(t, result.SuccessDeviceIdentifiers)
		assert.Empty(t, result.FailureDeviceIdentifiers)
		assert.NotNil(t, result.SuccessDeviceIdentifiers)
		assert.NotNil(t, result.FailureDeviceIdentifiers)
	})
}

func TestGetDeviceCount_NilIdentifiers(t *testing.T) {
	t.Run("handles nil success identifiers gracefully", func(t *testing.T) {
		// Arrange
		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             0,
			SuccessfulDevices:        0,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: nil,
			FailureDeviceIdentifiers: nil,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Equal(t, int64(0), result.Total)
		assert.Equal(t, int64(0), result.Success)
		assert.Equal(t, int64(0), result.Failure)
		assert.Nil(t, result.SuccessDeviceIdentifiers)
		assert.Nil(t, result.FailureDeviceIdentifiers)
	})
}

func TestGetDeviceCount_InvalidJSON(t *testing.T) {
	t.Run("handles malformed JSON in success identifiers", func(t *testing.T) {
		// Arrange
		malformedJSON := []byte(`{"invalid": "json"`)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             1,
			SuccessfulDevices:        1,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: malformedJSON,
			FailureDeviceIdentifiers: nil,
		}

		// Act
		result := getDeviceCount(row)

		// Assert - should gracefully handle error and not populate identifiers
		assert.Equal(t, int64(1), result.Total)
		assert.Equal(t, int64(1), result.Success)
		assert.Nil(t, result.SuccessDeviceIdentifiers)
	})
}

func TestGetDeviceCount_NonByteSliceType(t *testing.T) {
	t.Run("handles non-byte slice type for success identifiers", func(t *testing.T) {
		// Arrange - use string instead of []byte
		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             1,
			SuccessfulDevices:        1,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: "not a byte slice",
			FailureDeviceIdentifiers: nil,
		}

		// Act - should not panic
		result := getDeviceCount(row)

		// Assert
		assert.Equal(t, int64(1), result.Total)
		assert.Nil(t, result.SuccessDeviceIdentifiers)
	})
}

func TestGetDeviceCount_EmptyJSONArray(t *testing.T) {
	t.Run("handles empty JSON array for success identifiers", func(t *testing.T) {
		// Arrange
		emptyArray := []byte(`[]`)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             0,
			SuccessfulDevices:        0,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: emptyArray,
			FailureDeviceIdentifiers: nil,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Empty(t, result.SuccessDeviceIdentifiers)
		assert.NotNil(t, result.SuccessDeviceIdentifiers)
	})
}

func TestGetDeviceCount_ComplexScenarios(t *testing.T) {
	t.Run("handles mixed empty and valid strings in both arrays", func(t *testing.T) {
		// Arrange
		successIDs := []string{"device1", "", "device2", ""}
		failureIDs := []string{"", "device3", "", "device4", ""}

		successJSON, err := json.Marshal(successIDs)
		require.NoError(t, err)

		failureJSON, err := json.Marshal(failureIDs)
		require.NoError(t, err)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             4,
			SuccessfulDevices:        2,
			FailedDevices:            2,
			SuccessDeviceIdentifiers: successJSON,
			FailureDeviceIdentifiers: failureJSON,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Equal(t, []string{"device1", "device2"}, result.SuccessDeviceIdentifiers)
		assert.Equal(t, []string{"device3", "device4"}, result.FailureDeviceIdentifiers)
	})

	t.Run("preserves device identifier order", func(t *testing.T) {
		// Arrange
		successIDs := []string{"zebra", "apple", "mango", "banana"}
		jsonBytes, err := json.Marshal(successIDs)
		require.NoError(t, err)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             4,
			SuccessfulDevices:        4,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: jsonBytes,
			FailureDeviceIdentifiers: nil,
		}

		// Act
		result := getDeviceCount(row)

		// Assert - order should be preserved
		assert.Equal(t, successIDs, result.SuccessDeviceIdentifiers)
	})
}
