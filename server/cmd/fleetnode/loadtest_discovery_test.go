package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
	"github.com/stretchr/testify/require"
)

// The sandbox supplies its actual generated configuration and endpoint inventory.
// Keeping this in the owning Node package exercises the real CLI and probe boundary.
func TestLoadtestDiscoveryConfigContract(t *testing.T) {
	path := os.Getenv("FLEET_LOAD_DISCOVERY_CASES")
	if path == "" {
		t.Skip("set FLEET_LOAD_DISCOVERY_CASES to the sandbox's generated contract cases")
	}
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	var cases []struct {
		Name          string   `json:"name"`
		Configuration string   `json:"configuration"`
		Addresses     []string `json:"addresses"`
		Excluded      []string `json:"excluded"`
	}
	require.NoError(t, json.Unmarshal(body, &cases))
	require.NotEmpty(t, cases)
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			configuration := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(configuration, []byte(tc.Configuration), 0600))
			var cli CLI
			parser, err := kong.New(&cli, kong.Configuration(kongyaml.Loader, configuration), kong.Vars{"version": "test"})
			require.NoError(t, err)
			_, err = parser.Parse([]string{"run"})
			require.NoError(t, err)
			advertised, err := cli.Run.effectiveDiscoveryConfig()
			require.NoError(t, err)
			require.NotEmpty(t, advertised.GetRanges(), "advertised discovery ranges must cover the generated inventory")
			require.NotEmpty(t, tc.Addresses)
			require.NotEmpty(t, tc.Excluded)
			for batch := range slices.Chunk(tc.Addresses, 1024) {
				discoverer := &scopeRecordingDiscoverer{stubDiscoverer: &stubDiscoverer{}}
				cli.Run.discoverer = discoverer
				requested := append(slices.Clone(batch), tc.Excluded...)
				_, truncated, err := cli.Run.discoverForCommand(t.Context(), discoverIPList(requested, []string{"4028"}), discardLogger(t))
				require.NoError(t, err)
				require.False(t, truncated)
				want := make([]string, len(batch))
				for i, ip := range batch {
					want[i] = ip + "|4028"
				}
				require.ElementsMatch(t, want, discoverer.probed, "all in-scope endpoints and no excluded addresses reach plugins")
			}
			_, err = cli.Run.networkScanTargets(t.Context(), discoverNetworkScan(netscan.LocalSubnetTarget, []string{"4028"}).GetNetworkScan())
			require.ErrorContains(t, err, "automatic scan exceeds", "the full pair still needs bounded explicit discovery commands")
		})
	}
}
