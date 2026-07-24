package translator

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const managerTestURL = "stratum2+tcp://pool.example.com:34254/9bXiEd8boQVhq7WddEcERUL5tyyJVFYdU8th3HfbNXK3Yw6GRXh"

type managerTestRuntime struct {
	startCalls int
	stopCalls  int
	running    bool
}

func (f *managerTestRuntime) EnsureStarted(context.Context) error {
	f.startCalls++
	f.running = true
	return nil
}

func (f *managerTestRuntime) State(context.Context) (runtimeState, error) {
	return runtimeState{Exists: true, Running: f.running, Managed: true, Image: Image}, nil
}

func (f *managerTestRuntime) Stop(context.Context) error {
	f.stopCalls++
	f.running = false
	return nil
}

func TestApplyAssignmentRequiresReleaseBeforeProfileChange(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	listenerAddress, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
	port := listenerAddress.Port

	manager, err := NewManager(Config{
		StateDir:       t.TempDir(),
		AdvertisedHost: "sv2-tproxy",
		ConnectHost:    "127.0.0.1",
		DownstreamPort: uint16(port), // #nosec G115 -- ephemeral TCP ports fit in uint16.
		ReadyTimeout:   time.Second,
		HelperSocket:   DefaultHelperSocket,
	})
	require.NoError(t, err)
	runtime := &managerTestRuntime{}
	manager.runtime = runtime
	profile := Profile{Upstreams: []Upstream{{URL: managerTestURL, Username: "account"}}}

	endpoint, changed, err := manager.ApplyAssignment(
		context.Background(),
		&profile,
		Assignment{
			SelectedDeviceIdentifiers:   []string{"miner-b", "miner-a"},
			TranslatedDeviceIdentifiers: []string{"miner-b", "miner-a"},
		},
	)

	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "stratum+tcp://sv2-tproxy:"+strconv.Itoa(port), endpoint.String())
	assert.Equal(t, 1, runtime.startCalls)
	_, err = os.Stat(filepath.Join(manager.config.StateDir, configFileName))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(manager.config.StateDir, profileFileName))
	require.NoError(t, err)

	_, changed, err = manager.ApplyAssignment(
		context.Background(),
		&profile,
		Assignment{
			SelectedDeviceIdentifiers:   []string{"miner-a"},
			TranslatedDeviceIdentifiers: []string{"miner-a"},
		},
	)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, 2, runtime.startCalls)

	otherProfile := Profile{
		Upstreams: []Upstream{{
			URL:      "stratum2+tcp://other.example.com:34254/9bXiEd8boQVhq7WddEcERUL5tyyJVFYdU8th3HfbNXK3Yw6GRXh",
			Username: "other",
		}},
	}
	_, _, err = manager.ApplyAssignment(
		context.Background(),
		&otherProfile,
		Assignment{
			SelectedDeviceIdentifiers:   []string{"miner-a"},
			TranslatedDeviceIdentifiers: []string{"miner-a"},
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "move all translated miners")
	assert.Equal(t, 2, runtime.startCalls)
	assert.Equal(t, 0, runtime.stopCalls)

	_, changed, err = manager.ApplyAssignment(
		context.Background(),
		nil,
		Assignment{SelectedDeviceIdentifiers: []string{"miner-b"}},
	)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, 0, runtime.stopCalls)

	_, err = manager.PreviewAssignment(
		context.Background(),
		&otherProfile,
		Assignment{
			SelectedDeviceIdentifiers:   []string{"miner-a"},
			TranslatedDeviceIdentifiers: []string{"miner-a"},
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "move all translated miners")
	assert.Equal(t, 2, runtime.startCalls)
	assert.Equal(t, 0, runtime.stopCalls)

	_, changed, err = manager.ApplyAssignment(
		context.Background(),
		nil,
		Assignment{SelectedDeviceIdentifiers: []string{"miner-a"}},
	)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, 1, runtime.stopCalls)
	_, err = os.Stat(filepath.Join(manager.config.StateDir, configFileName))
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(manager.config.StateDir, profileFileName))
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, _, active := manager.ActiveProfile()
	assert.False(t, active)

	previewEndpoint, err := manager.PreviewAssignment(
		context.Background(),
		&otherProfile,
		Assignment{
			SelectedDeviceIdentifiers:   []string{"miner-a"},
			TranslatedDeviceIdentifiers: []string{"miner-a"},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, endpoint, previewEndpoint)
	assert.Equal(t, 2, runtime.startCalls)

	_, changed, err = manager.ApplyAssignment(
		context.Background(),
		&otherProfile,
		Assignment{
			SelectedDeviceIdentifiers:   []string{"miner-a"},
			TranslatedDeviceIdentifiers: []string{"miner-a"},
		},
	)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, 3, runtime.startCalls)
	assert.Equal(t, 1, runtime.stopCalls)
}

func TestPersistedAssignmentResumesTranslator(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	listenerAddress, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
	port := listenerAddress.Port
	stateDir := t.TempDir()
	config := Config{
		StateDir:       stateDir,
		AdvertisedHost: "10.0.0.5",
		ConnectHost:    "127.0.0.1",
		DownstreamPort: uint16(port), // #nosec G115 -- ephemeral TCP ports fit in uint16.
		ReadyTimeout:   time.Second,
		HelperSocket:   DefaultHelperSocket,
	}
	profile := Profile{Upstreams: []Upstream{{URL: managerTestURL, Username: "account"}}}
	manager, err := NewManager(config)
	require.NoError(t, err)
	manager.runtime = &managerTestRuntime{}
	_, _, err = manager.ApplyAssignment(
		context.Background(),
		&profile,
		Assignment{
			SelectedDeviceIdentifiers:   []string{"miner-a"},
			TranslatedDeviceIdentifiers: []string{"miner-a"},
		},
	)
	require.NoError(t, err)

	restarted, err := NewManager(config)
	require.NoError(t, err)
	runtime := &managerTestRuntime{}
	restarted.runtime = runtime

	require.NoError(t, restarted.Resume(context.Background()))
	assert.Equal(t, 1, runtime.startCalls)
	activeProfile, activeEndpoint, active := restarted.ActiveProfile()
	require.True(t, active)
	assert.True(t, ProfilesEqual(profile, activeProfile))
	assert.Equal(t, "stratum+tcp://10.0.0.5:"+strconv.Itoa(port), activeEndpoint.String())
}

func TestRenderConfigUsesCanonicalAuthorityKeyAndPerUpstreamIdentity(t *testing.T) {
	rendered, err := renderConfig(
		Profile{Upstreams: []Upstream{{
			URL:      managerTestURL,
			Username: "account.worker",
		}}},
		34255,
	)

	require.NoError(t, err)
	text := string(rendered)
	assert.Contains(t, text, "downstream_port = 34255")
	assert.Contains(t, text, "supported_extensions = [0x0002]")
	assert.Contains(t, text, `authority_pubkey = "9bXiEd8boQVhq7WddEcERUL5tyyJVFYdU8th3HfbNXK3Yw6GRXh"`)
	assert.Contains(t, text, `user_identity = "account.worker"`)
}
