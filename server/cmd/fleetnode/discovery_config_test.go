package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallDiscoveryConfigPersistsOnce(t *testing.T) {
	r := &RunCmd{state: &bootstrap.State{}, statePath: filepath.Join(t.TempDir(), "state.yaml")}
	require.NoError(t, r.installDiscoveryConfig([]string{"10.0.0.1-10.0.0.8"}))
	loaded, ok, err := bootstrap.LoadState(r.statePath)
	require.NoError(t, err)
	require.True(t, ok)
	restarted := &RunCmd{state: loaded}
	config, err := restarted.effectiveDiscoveryConfig()
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.1-10.0.0.8"}, config.Ranges)
	require.NoError(t, r.installDiscoveryConfig([]string{"10.0.0.1-10.0.0.8"}))
	require.Error(t, r.installDiscoveryConfig([]string{"10.1.0.1"}))
	require.Equal(t, []string{"10.0.0.1-10.0.0.8"}, r.state.DiscoveryRanges)
}

func TestInstallDiscoveryConfigRejectsConflictAndWriteFailure(t *testing.T) {
	r := &RunCmd{DiscoveryRanges: []string{"10.1.0.1"}, state: &bootstrap.State{}, statePath: filepath.Join(t.TempDir(), "state.yaml")}
	require.Error(t, r.installDiscoveryConfig([]string{"10.0.0.1"}))
	require.Empty(t, r.state.DiscoveryRanges)
	r.DiscoveryRanges = nil
	r.statePath = filepath.Join(t.TempDir(), "missing", "state.yaml", "not-a-directory")
	require.NoError(t, os.MkdirAll(filepath.Dir(r.statePath), 0700))
	require.NoError(t, os.WriteFile(r.statePath, []byte("existing"), 0600))
	// A directory cannot be atomically replaced by the state file.
	r.statePath = filepath.Dir(r.statePath)
	require.Error(t, r.installDiscoveryConfig([]string{"10.0.0.1"}))
	require.Empty(t, r.state.DiscoveryRanges)
}

func TestEffectiveDiscoveryConfig(t *testing.T) {
	tests := []struct {
		name       string
		configured []string
		legacy     string
		want       []string
	}{
		{name: "missing", want: []string{}},
		{name: "legacy subnet", legacy: "10.3.0.0/24", want: []string{"10.3.0.0-10.3.0.255"}},
		{name: "explicit overrides legacy", configured: []string{"10.2.0.1"}, legacy: "10.3.0.0/24", want: []string{"10.2.0.1"}},
		{name: "explicit empty overrides legacy", configured: []string{}, legacy: "10.3.0.0/24", want: []string{}},
		{name: "whitespace legacy disables", legacy: "  ", want: []string{}},
		{name: "invalid legacy disables", legacy: "8.8.8.8", want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			r := &RunCmd{
				DiscoveryRanges:      tt.configured,
				LocalDiscoverySubnet: tt.legacy,
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
