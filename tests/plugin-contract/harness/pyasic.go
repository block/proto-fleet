package harness

import (
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/proto-at-block/proto-fleet/server/sdk/v1"
)

const defaultPyasicConfig = `plugin:
  log_level: debug
  discovery_timeout_seconds: 10
  telemetry_cache_ttl_seconds: 1

miners:
  whatsminer:
    stock:
      enabled: true
`

func StartPyasic(t testing.TB) sdk.Driver {
	return StartPyasicWithConfig(t, defaultPyasicConfig)
}

func StartPyasicWithConfig(t testing.TB, yamlConfig string) sdk.Driver {
	t.Helper()

	if _, err := findPluginBinary("pyasic-plugin"); err != nil {
		t.Skipf("Skipping: %v", err)
	}

	// PyInstaller binary looks for pyasic-config.yaml beside the executable.
	binDir := pluginBinDir()
	configPath := filepath.Join(binDir, "pyasic-config.yaml")
	origData, origErr := os.ReadFile(configPath)

	if err := os.WriteFile(configPath, []byte(yamlConfig), 0644); err != nil {
		t.Fatalf("failed to write pyasic config: %v", err)
	}
	t.Cleanup(func() {
		if origErr == nil {
			os.WriteFile(configPath, origData, 0644)
		} else {
			os.Remove(configPath)
		}
	})

	return StartPlugin(t, "pyasic-plugin", configPath, nil)
}
