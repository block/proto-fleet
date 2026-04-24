package virtual

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/block/proto-fleet/plugin/virtual/internal/config"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func TestGenerateMetrics_StratumV2SupportReflectsConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.VirtualMinerConfig
		want sdk.StratumV2SupportStatus
	}{
		{
			name: "default miner reports unsupported",
			cfg: &config.VirtualMinerConfig{
				SerialNumber:        "default",
				BaselineHashrateTHS: 100, BaselinePowerW: 3000, BaselineTempC: 65,
				Hashboards: 1, ASICsPerBoard: 1, FanCount: 1, FanRPMMin: 100, FanRPMMax: 500,
			},
			want: sdk.StratumV2SupportUnsupported,
		},
		{
			name: "opt-in miner reports supported",
			cfg: &config.VirtualMinerConfig{
				SerialNumber:        "sv2",
				BaselineHashrateTHS: 100, BaselinePowerW: 3000, BaselineTempC: 65,
				Hashboards: 1, ASICsPerBoard: 1, FanCount: 1, FanRPMMin: 100, FanRPMMax: 500,
				StratumV2Supported: true,
			},
			want: sdk.StratumV2SupportSupported,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sim := NewSimulator(tc.cfg)
			metrics := sim.GenerateMetrics("dev-1", true)
			assert.Equal(t, tc.want, metrics.StratumV2Support)
		})
	}
}
