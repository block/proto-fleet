package fleetmanagement

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestFindMatchingFleetPoolID(t *testing.T) {
	fleetPools := []*poolspb.Pool{
		{PoolId: 1, Url: "stratum+tcp://pool1.example.com:3333", Username: "user1"},
		{PoolId: 2, Url: "stratum+tcp://pool2.example.com:3333", Username: "user2"},
		{PoolId: 3, Url: "stratum+tcp://pool3.example.com:3333", Username: "user3_long"},
	}

	tests := []struct {
		name           string
		url            string
		username       string
		fleetPools     []*poolspb.Pool
		expectedPoolID *int64
	}{
		{
			name:           "exact match on URL and username",
			url:            "stratum+tcp://pool1.example.com:3333",
			username:       "user1",
			fleetPools:     fleetPools,
			expectedPoolID: int64Ptr(1),
		},
		{
			name:           "match on username with device suffix",
			url:            "stratum+tcp://pool1.example.com:3333",
			username:       "user1.device123",
			fleetPools:     fleetPools,
			expectedPoolID: int64Ptr(1),
		},
		{
			name:     "match exact dotted legacy username before normalized fallback",
			url:      "stratum+tcp://pool4.example.com:3333",
			username: "user4.worker01",
			fleetPools: append(fleetPools, &poolspb.Pool{
				PoolId:   4,
				Url:      "stratum+tcp://pool4.example.com:3333",
				Username: "user4.worker01",
			}),
			expectedPoolID: int64Ptr(4),
		},
		{
			name:           "match with multiple dots in suffix",
			url:            "stratum+tcp://pool2.example.com:3333",
			username:       "user2.miner456.worker1",
			fleetPools:     fleetPools,
			expectedPoolID: int64Ptr(2),
		},
		{
			name:           "no match - URL mismatch",
			url:            "stratum+tcp://different-pool.example.com:3333",
			username:       "user1",
			fleetPools:     fleetPools,
			expectedPoolID: nil,
		},
		{
			name:           "no match - username mismatch",
			url:            "stratum+tcp://pool1.example.com:3333",
			username:       "different_user",
			fleetPools:     fleetPools,
			expectedPoolID: nil,
		},
		{
			name:           "no match - empty fleet pools",
			url:            "stratum+tcp://pool1.example.com:3333",
			username:       "user1",
			fleetPools:     []*poolspb.Pool{},
			expectedPoolID: nil,
		},
		{
			name:           "no match - nil fleet pools",
			url:            "stratum+tcp://pool1.example.com:3333",
			username:       "user1",
			fleetPools:     nil,
			expectedPoolID: nil,
		},
		{
			name:           "match with underscore in username",
			url:            "stratum+tcp://pool3.example.com:3333",
			username:       "user3_long.device1",
			fleetPools:     fleetPools,
			expectedPoolID: int64Ptr(3),
		},
		{
			name:     "match blank username exactly",
			url:      "stratum+tcp://pool4.example.com:3333",
			username: "   ",
			fleetPools: append(fleetPools, &poolspb.Pool{
				PoolId:   4,
				Url:      "stratum+tcp://pool4.example.com:3333",
				Username: "",
			}),
			expectedPoolID: int64Ptr(4),
		},
		{
			name:     "match blank username from leading-dot worker suffix",
			url:      "stratum+tcp://pool4.example.com:3333",
			username: ".worker-01",
			fleetPools: append(fleetPools, &poolspb.Pool{
				PoolId:   4,
				Url:      "stratum+tcp://pool4.example.com:3333",
				Username: "",
			}),
			expectedPoolID: int64Ptr(4),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := findMatchingFleetPoolID(tc.url, tc.username, tc.fleetPools)

			if tc.expectedPoolID == nil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, *tc.expectedPoolID, *result)
			}
		})
	}
}

func TestFindMatchingFleetPoolID_PrefixCollision(t *testing.T) {
	// Test case: when one pool's username is a prefix of another
	// The function extracts base username before "." and does exact match
	fleetPools := []*poolspb.Pool{
		{PoolId: 1, Url: "stratum+tcp://pool.example.com:3333", Username: "user"},
		{PoolId: 2, Url: "stratum+tcp://pool.example.com:3333", Username: "user_admin"},
	}

	// "user_admin.device1" extracts to "user_admin", matching pool 2 exactly
	result := findMatchingFleetPoolID(
		"stratum+tcp://pool.example.com:3333",
		"user_admin.device1",
		fleetPools,
	)

	assert.NotNil(t, result)
	assert.Equal(t, int64(2), *result)

	// "user.device1" extracts to "user", matching pool 1 exactly
	result = findMatchingFleetPoolID(
		"stratum+tcp://pool.example.com:3333",
		"user.device1",
		fleetPools,
	)

	assert.NotNil(t, result)
	assert.Equal(t, int64(1), *result)
}

func TestFindMatchingFleetPoolID_SimilarUsernames(t *testing.T) {
	// Test case: usernames like "ankit" and "ankit1" should not collide
	fleetPools := []*poolspb.Pool{
		{PoolId: 1, Url: "stratum+tcp://pool.example.com:3333", Username: "ankit"},
		{PoolId: 2, Url: "stratum+tcp://pool.example.com:3333", Username: "ankit1"},
	}

	// "ankit1.device123" should match pool 2 (ankit1), not pool 1 (ankit)
	result := findMatchingFleetPoolID(
		"stratum+tcp://pool.example.com:3333",
		"ankit1.device123",
		fleetPools,
	)

	assert.NotNil(t, result)
	assert.Equal(t, int64(2), *result)

	// "ankit.device123" should match pool 1 (ankit)
	result = findMatchingFleetPoolID(
		"stratum+tcp://pool.example.com:3333",
		"ankit.device123",
		fleetPools,
	)

	assert.NotNil(t, result)
	assert.Equal(t, int64(1), *result)
}

func TestIsMinerNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "fleeterror not found error",
			err:      fleeterror.NewNotFoundError("device not found: test-device-123"),
			expected: true,
		},
		{
			name:     "different error - no plugin available",
			err:      errors.New("no plugin available for miner type: ANTMINER"),
			expected: false,
		},
		{
			name:     "different error - database error",
			err:      errors.New("failed to get device data: connection refused"),
			expected: false,
		},
		{
			name:     "different error - empty device ID",
			err:      errors.New("device ID cannot be empty"),
			expected: false,
		},
		{
			name:     "plain string error with device not found - not matched",
			err:      errors.New("device not found"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isMinerNotFoundError(tc.err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func int64Ptr(i int64) *int64 {
	return &i
}
