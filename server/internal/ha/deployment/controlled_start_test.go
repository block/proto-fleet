package deployment

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
