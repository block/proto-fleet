// Package harness provides plugin subprocess launchers for contract tests.
//
// Each harness starts a plugin binary as a go-plugin subprocess, performs
// the handshake, and returns an SDK Driver client ready for testing.
package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func pluginBinDir() string {
	if dir := os.Getenv("PLUGIN_BIN_DIR"); dir != "" {
		return dir
	}
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(repoRoot, "server", "plugins")
}

func findPluginBinary(name string) (string, error) {
	path := filepath.Join(pluginBinDir(), name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("plugin binary %q not found at %s (run 'just build-plugins' first): %w", name, path, err)
	}
	return path, nil
}

// StartPlugin starts a plugin binary via go-plugin and returns the SDK Driver client.
func StartPlugin(t testing.TB, binaryName string, configPath string, env map[string]string) sdk.Driver {
	t.Helper()

	binPath, err := findPluginBinary(binaryName)
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}

	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PLUGIN_CONFIG_PATH=%s", configPath))
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	logger := hclog.New(&hclog.LoggerOptions{
		Name:   "test." + binaryName,
		Level:  hclog.Debug,
		Output: &testWriter{t: t},
	})

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  sdk.HandshakeConfig,
		Plugins:          sdk.PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		StartTimeout:     30 * time.Second,
		Logger:           logger,
	})

	t.Cleanup(func() {
		// go-plugin's Kill() hangs with PyInstaller binaries, so force-kill
		// the process after a short grace period.
		done := make(chan struct{})
		go func() {
			client.Kill()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}
	})

	rpcClient, err := client.Client()
	if err != nil {
		t.Fatalf("failed to connect to plugin %s: %v", binaryName, err)
	}

	raw, err := rpcClient.Dispense("driver")
	if err != nil {
		t.Fatalf("failed to dispense driver from %s: %v", binaryName, err)
	}

	driver, ok := raw.(sdk.Driver)
	if !ok {
		t.Fatalf("plugin %s does not implement Driver interface", binaryName)
	}

	return driver
}

type testWriter struct {
	t testing.TB
}

func (w *testWriter) Write(p []byte) (n int, err error) {
	w.t.Helper()
	w.t.Log(string(p))
	return len(p), nil
}
