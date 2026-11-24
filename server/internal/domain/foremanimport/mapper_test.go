package foremanimport

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/infrastructure/foreman"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseModel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard Antminer with firmware",
			input:    "Antminer S21 Pro (vnish)",
			expected: "S21 Pro",
		},
		{
			name:     "standard Whatsminer",
			input:    "Whatsminer M60S",
			expected: "M60S",
		},
		{
			name:     "Antminer with capacity suffix",
			input:    "Antminer S21 (200T)",
			expected: "S21",
		},
		{
			name:     "AvalonMiner with capacity",
			input:    "AvalonMiner 1466 (153T)",
			expected: "1466",
		},
		{
			name:     "single word returns as-is",
			input:    "BitAxe",
			expected: "BitAxe",
		},
		{
			name:     "empty string returns empty",
			input:    "",
			expected: "",
		},
		{
			name:     "two words no parentheses",
			input:    "Teraflux AT1500",
			expected: "AT1500",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			result := parseModel(tc.input)

			// Assert
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNormalizeForemanData_OmitsPoolsAndDoesNotInferWorkerNames(t *testing.T) {
	miners := []foreman.Miner{
		{ID: 1, Name: "North miner", Pools: []foreman.Pool{{URL: "pool.example:3333", Worker: "wallet.main.worker1"}}},
		{ID: 2, Pools: []foreman.Pool{{URL: "pool.example:3333", Worker: "wallet.main.worker2"}}},
		{ID: 3, Pools: []foreman.Pool{{URL: "pool.example:3333", Worker: "wallet.main.worker1"}}},
	}
	data := normalizeForemanData(miners, nil, nil)
	require.Empty(t, data.Pools, "Foreman import must not build configurations from observed pool usernames")
	require.Len(t, data.Miners, len(miners))
	assert.Equal(t, "North miner", data.Miners[0].Name)
	for _, miner := range data.Miners {
		assert.Empty(t, miner.WorkerName)
	}
	assert.Equal(t, "wallet.main.worker1", miners[0].Pools[0].Worker)
}
