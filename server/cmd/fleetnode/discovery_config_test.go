package main

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/stretchr/testify/require"
)

func TestEffectiveDiscoveryConfig(t *testing.T) {
	for _, tt := range []struct {
		name       string
		configured []string
		imported   []string
		want       []string
	}{
		{"missing", nil, nil, nil},
		{"imported", nil, []string{"10.1.0.1-10.1.0.2"}, []string{"10.1.0.1-10.1.0.2"}},
		{"explicit overrides imported", []string{"10.2.0.1"}, []string{"10.1.0.1"}, []string{"10.2.0.1"}},
		{"explicit empty disables", []string{}, []string{"10.1.0.1"}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := &RunCmd{DiscoveryRanges: tt.configured, state: &bootstrap.State{DiscoveryRanges: tt.imported}}
			config, err := r.effectiveDiscoveryConfig()
			require.NoError(t, err)
			require.ElementsMatch(t, tt.want, config.GetRanges())
		})
	}
	r := &RunCmd{DiscoveryRanges: []string{"8.8.8.8"}}
	_, err := r.effectiveDiscoveryConfig()
	require.Error(t, err)
}
