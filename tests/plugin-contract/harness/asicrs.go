package harness

import (
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const defaultAsicrsConfig = `plugin:
  log_level: debug
  discovery_timeout_seconds: 10
  telemetry_cache_ttl_seconds: 1

miners:
  whatsminer:
    stock:
      enabled: true
`

func StartAsicrs(t testing.TB) sdk.Driver {
	return StartAsicrsWithConfig(t, defaultAsicrsConfig)
}

func StartAsicrsWithConfig(t testing.TB, yamlConfig string) sdk.Driver {
	return StartAsicrsWithConfigAndEnv(t, yamlConfig, nil)
}

func StartAsicrsWithConfigAndEnv(t testing.TB, yamlConfig string, env map[string]string) sdk.Driver {
	t.Helper()

	if _, err := findPluginBinary("asicrs-plugin"); err != nil {
		t.Skipf("Skipping: %v", err)
	}

	binDir := pluginBinDir()
	configPath := filepath.Join(binDir, "asicrs-config.yaml")
	origData, origErr := os.ReadFile(configPath)

	if err := os.WriteFile(configPath, []byte(yamlConfig), 0644); err != nil {
		t.Fatalf("failed to write asicrs config: %v", err)
	}
	t.Cleanup(func() {
		if origErr == nil {
			os.WriteFile(configPath, origData, 0644)
		} else {
			os.Remove(configPath)
		}
	})

	return StartPlugin(t, "asicrs-plugin", configPath, env)
}
