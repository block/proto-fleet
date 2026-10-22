package deployment

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestControlledPassiveStartConsumesTokenOnlyOnce(t *testing.T) {
	// Execute the actual Compose entrypoint with lightweight sleep/fleetd stubs;
	// the second invocation models Docker's on-failure restart of the same command.
	model, err := os.ReadFile("../../../../deployment-files/ha/fleet-compose.yaml")
	require.NoError(t, err)
	var command []string
	for _, line := range strings.Split(string(model), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "command:") {
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "command:"))), &command))
			break
		}
	}
	require.Len(t, command, 3)
	root := t.TempDir()
	token := filepath.Join(root, "tokens", "passive")
	calls := filepath.Join(root, "calls")
	for _, name := range []string{"sleep", "fleetd"} {
		script := "#!/bin/sh\nprintf '" + name + "\\n' >> '" + calls + "'\n"
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(script), 0755))
	}
	shell := strings.ReplaceAll(command[2], "/run/proto-fleet-controlled-start/passive", token)
	shell = strings.ReplaceAll(shell, "/app/fleetd", filepath.Join(root, "fleetd"))
	start := func() error {
		cmd := exec.Command(command[0], command[1], shell)
		cmd.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"))
		return cmd.Run()
	}
	require.NoError(t, controlledPassiveStart(t.Context(), token, true, start))
	contents, err := os.ReadFile(calls)
	require.NoError(t, err)
	require.Equal(t, "fleetd\n", string(contents))
	require.NoError(t, start())
	contents, err = os.ReadFile(calls)
	require.NoError(t, err)
	require.Equal(t, "fleetd\nsleep\nfleetd\n", string(contents))
}

func TestControlledPassiveStartClearsTokenOnFailedOrNormalStart(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "controlled"}[skip], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "passive")
			require.NoError(t, os.WriteFile(path, []byte("stale"), 0600))
			startErr := errors.New("Compose failed before starting the container")
			err := controlledPassiveStart(t.Context(), path, skip, func() error {
				_, err := os.Stat(path)
				if skip {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, os.ErrNotExist)
				}
				return startErr
			})
			require.ErrorIs(t, err, startErr)
			_, err = os.Stat(path)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestControlledPassiveStartWaitsForAsynchronousContainerConsumption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passive")
	consumed := make(chan error, 1)
	err := controlledPassiveStart(t.Context(), path, true, func() error {
		go func() {
			time.Sleep(30 * time.Millisecond)
			consumed <- os.Remove(path)
		}()
		return nil // Docker can report running before scheduling the entrypoint.
	})
	require.NoError(t, err)
	require.NoError(t, <-consumed, "the entrypoint must consume the token before cleanup")
}

func TestApplicationCooldownRequiresStoppedPassiveAndActivePeer(t *testing.T) {
	active := fleetHostStatus{reachable: true, active: true, version: "v1.0.0"}
	for _, tc := range []struct {
		name      string
		passive   bool
		statusErr error
		peer      fleetHostStatus
		wantToken bool
		wantProbe bool
	}{
		{name: "controlled passive", passive: true, statusErr: fmt.Errorf("status: %w", syscall.ECONNREFUSED), peer: active, wantToken: true, wantProbe: true},
		{name: "ordinary recovery", statusErr: syscall.ECONNREFUSED, peer: active},
		{name: "running local", passive: true, peer: active},
		{name: "uncertain local", passive: true, statusErr: os.ErrPermission, peer: active},
		{name: "unreachable peer", passive: true, statusErr: syscall.ECONNREFUSED, peer: fleetHostStatus{active: true, version: "v1.0.0"}, wantProbe: true},
		{name: "passive peer", passive: true, statusErr: syscall.ECONNREFUSED, peer: fleetHostStatus{reachable: true, passive: true, version: "v1.0.0"}, wantProbe: true},
		{name: "unversioned peer", passive: true, statusErr: syscall.ECONNREFUSED, peer: fleetHostStatus{reachable: true, active: true}, wantProbe: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "passive")
			probed, started := false, false
			err := startApplicationWithCooldown(t.Context(), path, tc.passive, tc.statusErr, func() fleetHostStatus {
				probed = true
				return tc.peer
			}, func() error {
				started = true
				_, err := os.Stat(path)
				if tc.wantToken {
					require.NoError(t, err)
					return os.Remove(path)
				}
				require.ErrorIs(t, err, os.ErrNotExist)
				return nil
			})
			require.NoError(t, err)
			require.True(t, started)
			require.Equal(t, tc.wantProbe, probed)
		})
	}
}
