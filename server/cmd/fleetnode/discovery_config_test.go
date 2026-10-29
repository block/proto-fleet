package main

import (
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/stretchr/testify/require"
)

func TestEffectiveDiscoveryConfig(t *testing.T) {
	tests := []struct {
		name       string
		configured []string
		legacy     string
		imported   []string
		want       []string
	}{
		{name: "missing", want: []string{}},
		{name: "imported", imported: []string{"10.1.0.1-10.1.0.2"}, want: []string{"10.1.0.1-10.1.0.2"}},
		{name: "legacy subnet", legacy: "10.3.0.0/24", want: []string{"10.3.0.0-10.3.0.255"}},
		{name: "legacy subnet overrides imported", legacy: "10.3.0.0/24", imported: []string{"10.1.0.1"}, want: []string{"10.3.0.0-10.3.0.255"}},
		{name: "explicit overrides imported", configured: []string{"10.2.0.1"}, imported: []string{"10.1.0.1"}, want: []string{"10.2.0.1"}},
		{name: "explicit overrides legacy", configured: []string{"10.2.0.1"}, legacy: "10.3.0.0/24", want: []string{"10.2.0.1"}},
		{name: "explicit empty disables", configured: []string{}, imported: []string{"10.1.0.1"}, want: []string{}},
		{name: "explicit empty overrides legacy", configured: []string{}, legacy: "10.3.0.0/24", want: []string{}},
		{name: "whitespace legacy falls through to imported", legacy: "  ", imported: []string{"10.1.0.1"}, want: []string{"10.1.0.1"}},
		{name: "invalid legacy falls through to imported", legacy: "8.8.8.8", imported: []string{"10.1.0.1"}, want: []string{"10.1.0.1"}},
		{name: "invalid legacy without imported is empty", legacy: "not-a-subnet", want: []string{}},
		{
			name:       "unsorted overlapping input is merged and ordered",
			configured: []string{"10.0.0.128/25", "10.0.0.0/25", "192.168.1.1", "10.0.1.0-10.0.1.9", "10.0.1.5"},
			want:       []string{"10.0.0.0-10.0.1.9", "192.168.1.1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			r := &RunCmd{
				DiscoveryRanges:      tt.configured,
				LocalDiscoverySubnet: tt.legacy,
				state:                &bootstrap.State{DiscoveryRanges: tt.imported},
			}

			// Act
			config, err := r.effectiveDiscoveryConfig()

			// Assert
			require.NoError(t, err)
			require.Equal(t, tt.want, config.GetRanges())
		})
	}
}

func TestEffectiveDiscoveryConfigRejectsInvalidRanges(t *testing.T) {
	tooMany := make([]string, 0, 257)
	for i := range 257 {
		tooMany = append(tooMany, fmt.Sprintf("10.%d.%d.1", i/256, i%256))
	}
	tests := []struct {
		name     string
		r        *RunCmd
		contains string
	}{
		{name: "public explicit range", r: &RunCmd{DiscoveryRanges: []string{"8.8.8.8"}}, contains: "private"},
		{name: "public imported range", r: &RunCmd{state: &bootstrap.State{DiscoveryRanges: []string{"8.8.8.8"}}}, contains: "private"},
		{name: "more than 256 ranges", r: &RunCmd{DiscoveryRanges: tooMany}, contains: "at most 256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			_, err := tt.r.effectiveDiscoveryConfig()

			// Assert
			require.ErrorContains(t, err, tt.contains)
		})
	}
}
