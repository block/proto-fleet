package fleetmanagement

import (
	"testing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
)

func TestParseSortConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    []*pb.MinerSortConfig
		expected *interfaces.SortConfig
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty slice",
			input:    []*pb.MinerSortConfig{},
			expected: nil,
		},
		{
			name: "nil first element",
			input: []*pb.MinerSortConfig{
				nil,
			},
			expected: nil,
		},
		{
			name: "unspecified field",
			input: []*pb.MinerSortConfig{
				{
					Field:     pb.SortField_SORT_FIELD_UNSPECIFIED,
					Direction: pb.SortDirection_SORT_DIRECTION_ASC,
				},
			},
			expected: nil,
		},
		{
			name: "valid config",
			input: []*pb.MinerSortConfig{
				{
					Field:     pb.SortField_SORT_FIELD_HASHRATE,
					Direction: pb.SortDirection_SORT_DIRECTION_DESC,
				},
			},
			expected: &interfaces.SortConfig{
				Field:     interfaces.SortFieldHashrate,
				Direction: interfaces.SortDirectionDesc,
			},
		},
		{
			name: "unspecified direction defaults to ASC",
			input: []*pb.MinerSortConfig{
				{
					Field:     pb.SortField_SORT_FIELD_NAME,
					Direction: pb.SortDirection_SORT_DIRECTION_UNSPECIFIED,
				},
			},
			expected: &interfaces.SortConfig{
				Field:     interfaces.SortFieldName,
				Direction: interfaces.SortDirectionAsc,
			},
		},
		{
			name: "invalid field returns nil",
			input: []*pb.MinerSortConfig{
				{
					Field:     pb.SortField(100),
					Direction: pb.SortDirection_SORT_DIRECTION_ASC,
				},
			},
			expected: nil,
		},
		{
			name: "valid config - issues",
			input: []*pb.MinerSortConfig{
				{
					Field:     pb.SortField_SORT_FIELD_ISSUES,
					Direction: pb.SortDirection_SORT_DIRECTION_ASC,
				},
			},
			expected: &interfaces.SortConfig{
				Field:     interfaces.SortFieldIssues,
				Direction: interfaces.SortDirectionAsc,
			},
		},
		{
			name: "valid config - firmware",
			input: []*pb.MinerSortConfig{
				{
					Field:     pb.SortField_SORT_FIELD_FIRMWARE,
					Direction: pb.SortDirection_SORT_DIRECTION_DESC,
				},
			},
			expected: &interfaces.SortConfig{
				Field:     interfaces.SortFieldFirmware,
				Direction: interfaces.SortDirectionDesc,
			},
		},
		{
			name: "uses first element only (multi-column reserved for future)",
			input: []*pb.MinerSortConfig{
				{
					Field:     pb.SortField_SORT_FIELD_HASHRATE,
					Direction: pb.SortDirection_SORT_DIRECTION_DESC,
				},
				{
					Field:     pb.SortField_SORT_FIELD_NAME,
					Direction: pb.SortDirection_SORT_DIRECTION_ASC,
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
