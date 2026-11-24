package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryRangesFromOperatorYAML(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		want         []string
	}{
		{"configured", "run:\n  discovery-ranges:\n    - 10.0.0.1-10.0.15.254\n", []string{"10.0.0.1-10.0.15.254"}},
		{"explicit empty", "run:\n  discovery-ranges: []\n", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.source), 0600))
			var cli CLI
			parser, err := kong.New(&cli, kong.Configuration(kongyaml.Loader, path), kong.Vars{"version": "test"})
			require.NoError(t, err)
			_, err = parser.Parse([]string{"run"})
			require.NoError(t, err)
			cli.Run.LocalDiscoverySubnet = "10.1.0.0/24"

			// Act
			effective, err := cli.Run.effectiveDiscoveryConfig()

			// Assert
			require.NoError(t, err)
			require.ElementsMatch(t, tt.want, effective.GetRanges())
		})
	}
}
