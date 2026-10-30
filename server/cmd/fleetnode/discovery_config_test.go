package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	fleetnodecontrol "github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/stretchr/testify/require"
)

func TestInstallDiscoveryConfigPersistsOnce(t *testing.T) {
	// Arrange
	r := &RunCmd{state: &bootstrap.State{}, statePath: filepath.Join(t.TempDir(), "state.yaml")}

	// Act
	firstErr := r.installDiscoveryConfig([]string{"10.0.0.1-10.0.0.8"})
	loaded, ok, loadErr := bootstrap.LoadState(r.statePath)
	repeatErr := r.installDiscoveryConfig([]string{"10.0.0.1-10.0.0.8"})
	conflictErr := r.installDiscoveryConfig([]string{"10.1.0.1"})

	// Assert
	require.NoError(t, firstErr)
	require.NoError(t, loadErr)
	require.True(t, ok)
	config, err := (&RunCmd{state: loaded}).effectiveDiscoveryConfig()
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.1-10.0.0.8"}, config.Ranges)
	require.NoError(t, repeatErr)
	require.Error(t, conflictErr)
	require.Equal(t, []string{"10.0.0.1-10.0.0.8"}, r.state.DiscoveryRanges)
}

func TestInstallDiscoveryConfigRejectsConflictAndWriteFailure(t *testing.T) {
	// Arrange
	r := &RunCmd{DiscoveryRanges: []string{"10.1.0.1"}, state: &bootstrap.State{}, statePath: filepath.Join(t.TempDir(), "state.yaml")}

	// Act
	conflictErr := r.installDiscoveryConfig([]string{"10.0.0.1"})

	// Assert
	require.ErrorContains(t, conflictErr, fleetnodecontrol.StandbyDiscoveryConflictMessage)
	require.Empty(t, r.state.DiscoveryRanges)

	// Arrange: a directory cannot be atomically replaced by the state file.
	r.DiscoveryRanges = nil
	blocked := filepath.Join(t.TempDir(), "missing", "state.yaml", "not-a-directory")
	require.NoError(t, os.MkdirAll(filepath.Dir(blocked), 0700))
	require.NoError(t, os.WriteFile(blocked, []byte("existing"), 0600))
	r.statePath = filepath.Dir(blocked)

	// Act
	writeErr := r.installDiscoveryConfig([]string{"10.0.0.1"})

	// Assert
	require.Error(t, writeErr)
	require.Empty(t, r.state.DiscoveryRanges)
}

func TestInstallDiscoveryConfigTreatsOnlyUsableLegacySubnetAsLocalConfig(t *testing.T) {
	tests := []struct {
		name         string
		legacy       string
		wantConflict bool
	}{
		{name: "whitespace is ignored", legacy: "  "},
		{name: "invalid subnet is ignored", legacy: "not-a-subnet"},
		{name: "usable subnet that differs conflicts", legacy: "10.3.0.0/24", wantConflict: true},
		{name: "usable subnet that matches installs", legacy: "10.0.0.0/24"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			r := &RunCmd{LocalDiscoverySubnet: tt.legacy, state: &bootstrap.State{}, statePath: filepath.Join(t.TempDir(), "state.yaml")}

			// Act
			err := r.installDiscoveryConfig([]string{"10.0.0.0-10.0.0.255"})

			// Assert
			if tt.wantConflict {
				require.ErrorContains(t, err, fleetnodecontrol.StandbyDiscoveryConflictMessage)
				require.Empty(t, r.state.DiscoveryRanges)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{"10.0.0.0-10.0.0.255"}, r.state.DiscoveryRanges)
		})
	}
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
