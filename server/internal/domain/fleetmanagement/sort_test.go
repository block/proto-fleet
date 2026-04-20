package fleetmanagement

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
)

func TestParseSortConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    []*commonpb.SortConfig
		expected *interfaces.SortConfig
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty slice",
			input:    []*commonpb.SortConfig{},
			expected: nil,
		},
		{
			name: "nil first element",
			input: []*commonpb.SortConfig{
				nil,
			},
			expected: nil,
		},
		{
			name: "unspecified field",
			input: []*commonpb.SortConfig{
				{
					Field:     commonpb.SortField_SORT_FIELD_UNSPECIFIED,
					Direction: commonpb.SortDirection_SORT_DIRECTION_ASC,
				},
			},
			expected: nil,
		},
		{
			name: "valid config",
			input: []*commonpb.SortConfig{
				{
					Field:     commonpb.SortField_SORT_FIELD_HASHRATE,
					Direction: commonpb.SortDirection_SORT_DIRECTION_DESC,
				},
			},
			expected: &interfaces.SortConfig{
				Field:     interfaces.SortFieldHashrate,
				Direction: interfaces.SortDirectionDesc,
			},
		},
		{
			name: "unspecified direction defaults to ASC",
			input: []*commonpb.SortConfig{
				{
					Field:     commonpb.SortField_SORT_FIELD_NAME,
					Direction: commonpb.SortDirection_SORT_DIRECTION_UNSPECIFIED,
				},
			},
			expected: &interfaces.SortConfig{
				Field:     interfaces.SortFieldName,
				Direction: interfaces.SortDirectionAsc,
			},
		},
		{
			name: "invalid field returns nil",
			input: []*commonpb.SortConfig{
				{
					Field:     commonpb.SortField(100),
					Direction: commonpb.SortDirection_SORT_DIRECTION_ASC,
				},
			},
			expected: nil,
		},
		{
			name: "collection-only issue count sort returns nil",
			input: []*commonpb.SortConfig{
				{
					Field:     commonpb.SortField_SORT_FIELD_ISSUE_COUNT,
					Direction: commonpb.SortDirection_SORT_DIRECTION_DESC,
				},
			},
			expected: nil,
		},
		{
			name: "collection-only location sort returns nil",
			input: []*commonpb.SortConfig{
				{
					Field:     commonpb.SortField_SORT_FIELD_LOCATION,
					Direction: commonpb.SortDirection_SORT_DIRECTION_ASC,
				},
			},
			expected: nil,
		},
		{
			name: "valid config - firmware",
			input: []*commonpb.SortConfig{
				{
					Field:     commonpb.SortField_SORT_FIELD_FIRMWARE,
					Direction: commonpb.SortDirection_SORT_DIRECTION_DESC,
				},
			},
			expected: &interfaces.SortConfig{
				Field:     interfaces.SortFieldFirmware,
				Direction: interfaces.SortDirectionDesc,
			},
		},
		{
			name: "uses first element only (multi-column reserved for future)",
			input: []*commonpb.SortConfig{
				{
					Field:     commonpb.SortField_SORT_FIELD_HASHRATE,
					Direction: commonpb.SortDirection_SORT_DIRECTION_DESC,
				},
				{
					Field:     commonpb.SortField_SORT_FIELD_NAME,
					Direction: commonpb.SortDirection_SORT_DIRECTION_ASC,
				},
			},
			expected: &interfaces.SortConfig{
				Field:     interfaces.SortFieldHashrate,
				Direction: interfaces.SortDirectionDesc,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseSortConfig(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
