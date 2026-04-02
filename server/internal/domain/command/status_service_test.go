package command

import (
	"encoding/json"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDeviceCount_ValidSuccessIdentifiers(t *testing.T) {
	t.Run("parses valid success device identifiers", func(t *testing.T) {
		// Arrange
		successIDs := []string{"device1", "device2", "device3"}
		jsonBytes, err := json.Marshal(successIDs)
		require.NoError(t, err)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             3,
			SuccessfulDevices:        3,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: jsonBytes,
			FailureDeviceIdentifiers: nil,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Equal(t, int64(3), result.Total)
		assert.Equal(t, int64(3), result.Success)
		assert.Equal(t, int64(0), result.Failure)
		assert.Equal(t, successIDs, result.SuccessDeviceIdentifiers)
		assert.Empty(t, result.FailureDeviceIdentifiers)
	})
}

func TestGetDeviceCount_ValidFailureIdentifiers(t *testing.T) {
	t.Run("parses valid failure device identifiers", func(t *testing.T) {
		// Arrange
		failureIDs := []string{"device4", "device5"}
		jsonBytes, err := json.Marshal(failureIDs)
		require.NoError(t, err)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             2,
			SuccessfulDevices:        0,
			FailedDevices:            2,
			SuccessDeviceIdentifiers: nil,
			FailureDeviceIdentifiers: jsonBytes,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Equal(t, int64(2), result.Total)
		assert.Equal(t, int64(0), result.Success)
		assert.Equal(t, int64(2), result.Failure)
		assert.Empty(t, result.SuccessDeviceIdentifiers)
		assert.Equal(t, failureIDs, result.FailureDeviceIdentifiers)
	})
}

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
	t.Run("filters out empty strings from success identifiers", func(t *testing.T) {
		// Arrange - JSON_ARRAYAGG can include null values as empty strings
		successIDsWithNulls := []string{"device1", "", "device2", "", "device3"}
		jsonBytes, err := json.Marshal(successIDsWithNulls)
		require.NoError(t, err)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             3,
			SuccessfulDevices:        3,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: jsonBytes,
			FailureDeviceIdentifiers: nil,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		expectedIDs := []string{"device1", "device2", "device3"}
		assert.Equal(t, expectedIDs, result.SuccessDeviceIdentifiers)
		assert.Len(t, result.SuccessDeviceIdentifiers, 3)
	})

	t.Run("filters out empty strings from failure identifiers", func(t *testing.T) {
		// Arrange
		failureIDsWithNulls := []string{"", "device1", "", "device2"}
		jsonBytes, err := json.Marshal(failureIDsWithNulls)
		require.NoError(t, err)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             2,
			SuccessfulDevices:        0,
			FailedDevices:            2,
			SuccessDeviceIdentifiers: nil,
			FailureDeviceIdentifiers: jsonBytes,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		expectedIDs := []string{"device1", "device2"}
		assert.Equal(t, expectedIDs, result.FailureDeviceIdentifiers)
		assert.Len(t, result.FailureDeviceIdentifiers, 2)
	})

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

	t.Run("handles nil success identifiers with valid failure identifiers", func(t *testing.T) {
		// Arrange
		failureIDs := []string{"device1"}
		failureJSON, err := json.Marshal(failureIDs)
		require.NoError(t, err)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             1,
			SuccessfulDevices:        0,
			FailedDevices:            1,
			SuccessDeviceIdentifiers: nil,
			FailureDeviceIdentifiers: failureJSON,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Nil(t, result.SuccessDeviceIdentifiers)
		assert.Equal(t, failureIDs, result.FailureDeviceIdentifiers)
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

	t.Run("handles malformed JSON in failure identifiers", func(t *testing.T) {
		// Arrange
		malformedJSON := []byte(`["incomplete array"`)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             1,
			SuccessfulDevices:        0,
			FailedDevices:            1,
			SuccessDeviceIdentifiers: nil,
			FailureDeviceIdentifiers: malformedJSON,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Equal(t, int64(1), result.Total)
		assert.Equal(t, int64(1), result.Failure)
		assert.Nil(t, result.FailureDeviceIdentifiers)
	})

	t.Run("handles wrong JSON type (object instead of array)", func(t *testing.T) {
		// Arrange
		wrongTypeJSON := []byte(`{"key": "value"}`)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             0,
			SuccessfulDevices:        0,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: wrongTypeJSON,
			FailureDeviceIdentifiers: wrongTypeJSON,
		}

		// Act
		result := getDeviceCount(row)

		// Assert - should fail to unmarshal and leave identifiers nil
		assert.Nil(t, result.SuccessDeviceIdentifiers)
		assert.Nil(t, result.FailureDeviceIdentifiers)
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

	t.Run("handles non-byte slice type for failure identifiers", func(t *testing.T) {
		// Arrange - use integer instead of []byte
		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             1,
			SuccessfulDevices:        0,
			FailedDevices:            1,
			SuccessDeviceIdentifiers: nil,
			FailureDeviceIdentifiers: 12345,
		}

		// Act - should not panic
		result := getDeviceCount(row)

		// Assert
		assert.Equal(t, int64(1), result.Total)
		assert.Nil(t, result.FailureDeviceIdentifiers)
	})

	t.Run("handles non-byte slice type for both identifiers", func(t *testing.T) {
		// Arrange
		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             2,
			SuccessfulDevices:        1,
			FailedDevices:            1,
			SuccessDeviceIdentifiers: []int{1, 2, 3},
			FailureDeviceIdentifiers: map[string]string{"key": "value"},
		}

		// Act - should not panic
		result := getDeviceCount(row)

		// Assert
		assert.Equal(t, int64(2), result.Total)
		assert.Nil(t, result.SuccessDeviceIdentifiers)
		assert.Nil(t, result.FailureDeviceIdentifiers)
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

	t.Run("handles empty JSON array for failure identifiers", func(t *testing.T) {
		// Arrange
		emptyArray := []byte(`[]`)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             0,
			SuccessfulDevices:        0,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: nil,
			FailureDeviceIdentifiers: emptyArray,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Empty(t, result.FailureDeviceIdentifiers)
		assert.NotNil(t, result.FailureDeviceIdentifiers)
	})
}

func TestGetDeviceCount_ComplexScenarios(t *testing.T) {
	t.Run("handles large number of device identifiers", func(t *testing.T) {
		// Arrange
		largeSuccessIDs := make([]string, 1000)
		for i := range 1000 {
			largeSuccessIDs[i] = "device" + string(rune(i))
		}

		jsonBytes, err := json.Marshal(largeSuccessIDs)
		require.NoError(t, err)

		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             1000,
			SuccessfulDevices:        1000,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: jsonBytes,
			FailureDeviceIdentifiers: nil,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Len(t, result.SuccessDeviceIdentifiers, 1000)
	})

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

func TestGetDeviceCount_DeviceCounts(t *testing.T) {
	t.Run("correctly maps device counts to protobuf fields", func(t *testing.T) {
		// Arrange
		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             10,
			SuccessfulDevices:        7,
			FailedDevices:            3,
			SuccessDeviceIdentifiers: nil,
			FailureDeviceIdentifiers: nil,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.Equal(t, int64(10), result.Total)
		assert.Equal(t, int64(7), result.Success)
		assert.Equal(t, int64(3), result.Failure)
	})

	t.Run("returns correct protobuf message type", func(t *testing.T) {
		// Arrange
		row := &sqlc.GetBatchStatusAndDeviceCountsRow{
			DevicesCount:             1,
			SuccessfulDevices:        1,
			FailedDevices:            0,
			SuccessDeviceIdentifiers: nil,
			FailureDeviceIdentifiers: nil,
		}

		// Act
		result := getDeviceCount(row)

		// Assert
		assert.IsType(t, &pb.CommandBatchUpdateDeviceCount{}, result)
	})
}
