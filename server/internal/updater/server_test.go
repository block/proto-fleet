package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/updaterapi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleUpgradeRejectsTrailingContentAndAllowsWhitespace(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		body      string
		wantError string
	}{
		{
			name:      "second JSON value",
			body:      `{"target_version":"v1.2.3"}{}`,
			wantError: "invalid request body",
		},
		{
			name:      "trailing junk",
			body:      `{"target_version":"v1.2.3"} trailing`,
			wantError: "invalid request body",
		},
		{
			name:      "trailing whitespace",
			body:      "{\"operation_id\":\"11111111-1111-4111-8111-111111111111\",\"target_version\":\"invalid\"}\n\t ",
			wantError: "target version must be a stable or RC release tag",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/upgrade", strings.NewReader(test.body))
			NewServer(&Manager{}).handleUpgrade(recorder, request)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), test.wantError)
		})
	}
}

func TestHandleUpgradeMapsManagerStateWithoutStatusSnapshot(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name             string
		targetVersion    string
		phase            updaterapi.Phase
		operationRunning bool
		closing          bool
		wantStatus       int
	}{
		{
			name:             "active operation",
			targetVersion:    "v1.2.0",
			phase:            updaterapi.PhasePreflight,
			operationRunning: true,
			wantStatus:       http.StatusConflict,
		},
		{
			name:             "terminal operation still cleaning up",
			targetVersion:    "v1.2.0",
			phase:            updaterapi.PhaseSucceeded,
			operationRunning: true,
			wantStatus:       http.StatusConflict,
		},
		{
			name:          "updater closing",
			targetVersion: "v1.2.0",
			closing:       true,
			wantStatus:    http.StatusServiceUnavailable,
		},
		{
			name:             "invalid target while operation active",
			targetVersion:    "invalid",
			phase:            updaterapi.PhasePreflight,
			operationRunning: true,
			wantStatus:       http.StatusBadRequest,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			installRoot := t.TempDir()
			writeCurrentDeployment(t, installRoot, "v1.0.0")
			manager, err := NewManager(Config{
				InstallRoot: installRoot,
				StateDir:    filepath.Join(t.TempDir(), "state"),
				GOARCH:      "amd64",
			})
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, manager.Close()) })

			manager.mu.Lock()
			if test.phase != "" {
				manager.operation = &updaterapi.Operation{
					TargetVersion: "v1.1.0",
					Phase:         test.phase,
				}
			}
			manager.operationRunning = test.operationRunning
			manager.closing = test.closing
			manager.mu.Unlock()

			recorder := httptest.NewRecorder()
			body := fmt.Sprintf(
				`{"operation_id":"11111111-1111-4111-8111-111111111111","target_version":%q}`,
				test.targetVersion,
			)
			request := httptest.NewRequest(http.MethodPost, "/v1/upgrade", strings.NewReader(body))
			NewServer(manager).handleUpgrade(recorder, request)

			assert.Equal(t, test.wantStatus, recorder.Code)
		})
	}
}

func TestHandleUpgradeUsesCallerOperationID(t *testing.T) {
	t.Parallel()

	operationID := "11111111-1111-4111-8111-111111111111"
	manager := &Manager{operation: &updaterapi.Operation{
		ID:            operationID,
		TargetVersion: "v1.1.0",
		Phase:         updaterapi.PhaseQueued,
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/upgrade",
		strings.NewReader(fmt.Sprintf(`{"operation_id":%q,"target_version":"v1.1.0"}`, operationID)),
	)
	NewServer(manager).handleUpgrade(recorder, request)

	assert.Equal(t, http.StatusAccepted, recorder.Code)
	var response updaterapi.TriggerResponse
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&response))
	assert.Equal(t, operationID, response.Operation.ID)
}

func TestHandleUpgradeRejectsInvalidOperationID(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/upgrade",
		strings.NewReader(`{"operation_id":"not-a-uuid","target_version":"v1.1.0"}`),
	)
	NewServer(&Manager{}).handleUpgrade(recorder, request)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "operation id must be a canonical UUID")
}

func TestHandleUpgradeRequiresOperationID(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"target_version":"v1.1.0"}`,
		`{"operation_id":"","target_version":"v1.1.0"}`,
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/upgrade", strings.NewReader(body))
		NewServer(&Manager{}).handleUpgrade(recorder, request)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "operation id must be a canonical UUID")
	}
}

func TestTriggerErrorHTTPStatus(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid", err: newTriggerError(errTriggerInvalid, "invalid"), want: http.StatusBadRequest},
		{name: "precondition", err: newTriggerError(errTriggerPrecondition, "precondition"), want: http.StatusPreconditionFailed},
		{name: "busy", err: newTriggerError(errTriggerBusy, "busy"), want: http.StatusConflict},
		{name: "closing", err: errTriggerClosing, want: http.StatusServiceUnavailable},
		{name: "internal", err: assert.AnError, want: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, triggerErrorHTTPStatus(test.err))
		})
	}
}

func TestHandleUpgradeSanitizesInternalManagerError(t *testing.T) {
	t.Parallel()

	missingInstallRoot := filepath.Join(t.TempDir(), "privileged-host-path")
	manager := &Manager{cfg: Config{
		InstallRoot: missingInstallRoot,
		StateDir:    t.TempDir(),
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/upgrade",
		strings.NewReader(`{"operation_id":"11111111-1111-4111-8111-111111111111","target_version":"v1.1.0"}`),
	)
	NewServer(manager).handleUpgrade(recorder, request)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "host updater failed to start upgrade")
	assert.NotContains(t, recorder.Body.String(), missingInstallRoot)
}

func TestServerReadyAfterSocketIsBoundAndSecured(t *testing.T) {
	t.Parallel()

	socketPath := shortSocketPath(t)
	server := NewServer(nil)
	done := make(chan error, 1)
	go func() { done <- server.Serve(socketPath) }()

	select {
	case <-server.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("server did not report readiness")
	}
	info, err := os.Lstat(socketPath)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSocket)
	assert.Equal(t, os.FileMode(0o660), info.Mode().Perm())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, server.Shutdown(shutdownCtx))
	require.ErrorIs(t, <-done, http.ErrServerClosed)
}

func TestServerDoesNotReportReadinessWhenSocketSetupFails(t *testing.T) {
	t.Parallel()

	server := NewServer(nil)
	require.ErrorContains(t, server.Serve("relative.sock"), "socket path must be absolute")
	select {
	case <-server.Ready():
		t.Fatal("failed server startup reported readiness")
	default:
	}
}

func TestListenUpdaterSocketRefusesLiveListener(t *testing.T) {
	t.Parallel()

	socketPath := shortSocketPath(t)
	existing, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, existing.Close()) })

	_, err = listenUpdaterSocket(socketPath)
	require.ErrorContains(t, err, "another updater is already listening")
	connection, err := net.DialTimeout("unix", socketPath, time.Second)
	require.NoError(t, err, "the live listener path must not be unlinked")
	require.NoError(t, connection.Close())
}

func TestListenUpdaterSocketReclaimsStaleSocket(t *testing.T) {
	t.Parallel()

	socketPath := shortSocketPath(t)
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	require.NoError(t, err)
	stale.SetUnlinkOnClose(false)
	require.NoError(t, stale.Close())
	assert.FileExists(t, socketPath)

	var listener *net.UnixListener
	// Darwin can briefly continue accepting connects immediately after Close.
	// The production code correctly treats that ambiguity as live, so retry the
	// proof rather than weakening its fail-closed behavior.
	require.Eventually(t, func() bool {
		listener, err = listenUpdaterSocket(socketPath)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, listener.Close())
	assert.NoFileExists(t, socketPath)
}

func TestListenUpdaterSocketRefusesNonSocketPath(t *testing.T) {
	t.Parallel()

	socketPath := shortSocketPath(t)
	require.NoError(t, os.WriteFile(socketPath, []byte("not a socket"), 0o600))

	_, err := listenUpdaterSocket(socketPath)
	require.ErrorContains(t, err, "refusing to replace non-socket path")
	assert.Equal(t, "not a socket", mustReadFile(t, socketPath))
}

func TestPrepareUpdaterSocketDirectoryRefusesSymlink(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "proto-fleet-updater-dir-test-") //nolint:usetesting
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(root)) })
	realDirectory := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(realDirectory, 0o700))
	symlinkDirectory := filepath.Join(root, "socket-dir")
	require.NoError(t, os.Symlink(realDirectory, symlinkDirectory))

	_, err = prepareUpdaterSocketDirectory(filepath.Join(symlinkDirectory, "updater.sock"))
	require.ErrorContains(t, err, "must not be a symlink")
}

func TestPrepareUpdaterSocketDirectoryRefusesWritableDirectory(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "group writable", mode: 0o770},
		{name: "world writable even when sticky", mode: 0o777 | os.ModeSticky},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			socketPath := shortSocketPath(t)
			require.NoError(t, os.Chmod(filepath.Dir(socketPath), test.mode))

			_, err := prepareUpdaterSocketDirectory(socketPath)
			require.ErrorContains(t, err, "must not be group- or world-writable")
		})
	}
}

func TestPrepareUpdaterSocketDirectoryAllowsStickySharedAncestor(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "proto-fleet-updater-dir-test-") //nolint:usetesting
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(root)) })
	sharedDirectory := filepath.Join(root, "shared")
	require.NoError(t, os.Mkdir(sharedDirectory, 0o700))
	require.NoError(t, os.Chmod(sharedDirectory, 0o777|os.ModeSticky))
	socketDirectory := filepath.Join(sharedDirectory, "socket-dir")
	require.NoError(t, os.Mkdir(socketDirectory, 0o700))

	_, err = prepareUpdaterSocketDirectory(filepath.Join(socketDirectory, "updater.sock"))
	require.NoError(t, err)
}

func TestValidateUpdaterSocketPathComponentRefusesUntrustedAncestorSymlink(t *testing.T) {
	t.Parallel()

	daemonUID := uint32(os.Geteuid()) //nolint:gosec // Effective UIDs are non-negative on supported Unix hosts.
	untrustedUID := uint32(1)
	if daemonUID == untrustedUID {
		untrustedUID++
	}

	err := validateUpdaterSocketPathComponent(
		"/shared/attacker-link",
		os.ModeSymlink,
		untrustedUID,
		daemonUID,
		false,
	)
	require.ErrorContains(t, err, "owner uid")
}

func TestPrepareUpdaterSocketDirectoryReturnsCanonicalPath(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "proto-fleet-updater-dir-test-") //nolint:usetesting
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(root)) })
	originalParent := filepath.Join(root, "original")
	replacementParent := filepath.Join(root, "replacement")
	for _, parent := range []string{originalParent, replacementParent} {
		require.NoError(t, os.MkdirAll(filepath.Join(parent, "socket-dir"), 0o700))
	}
	linkedParent := filepath.Join(root, "linked")
	require.NoError(t, os.Symlink(originalParent, linkedParent))
	configuredSocketPath := filepath.Join(linkedParent, "socket-dir", "updater.sock")

	canonicalSocketPath, err := prepareUpdaterSocketDirectory(configuredSocketPath)
	require.NoError(t, err)
	canonicalOriginalDirectory, err := filepath.EvalSymlinks(filepath.Join(originalParent, "socket-dir"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(canonicalOriginalDirectory, "updater.sock"), canonicalSocketPath)

	// Once validation is complete, later operations use the canonical path and
	// cannot be redirected by resolving the configured symlink again.
	require.NoError(t, os.Remove(linkedParent))
	require.NoError(t, os.Symlink(replacementParent, linkedParent))
	listener, err := listenUpdaterSocket(canonicalSocketPath)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, listener.Close()) })
	require.NoError(t, secureUpdaterSocket(canonicalSocketPath))
	assert.FileExists(t, filepath.Join(canonicalOriginalDirectory, "updater.sock"))
	assert.NoFileExists(t, filepath.Join(replacementParent, "socket-dir", "updater.sock"))
}

func TestSecureUpdaterSocketOverridesSetgidDirectoryAndSetsMode(t *testing.T) {
	t.Parallel()

	socketPath := shortSocketPath(t)
	directory := filepath.Dir(socketPath)
	inheritedGID := alternateOwnedGroup(t)
	require.NoError(t, os.Chown(directory, os.Geteuid(), inheritedGID))
	require.NoError(t, os.Chmod(directory, 0o750|os.ModeSetgid))
	directoryInfo, err := os.Lstat(directory)
	require.NoError(t, err)
	require.NotZero(t, directoryInfo.Mode()&os.ModeSetgid)
	canonicalSocketPath, err := prepareUpdaterSocketDirectory(socketPath)
	require.NoError(t, err)

	listener, err := listenUpdaterSocket(canonicalSocketPath)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, listener.Close()) })
	if inheritedGID != os.Getegid() {
		inheritedInfo, err := os.Lstat(canonicalSocketPath)
		require.NoError(t, err)
		inheritedStat, ok := inheritedInfo.Sys().(*syscall.Stat_t)
		require.True(t, ok)
		require.Equal(t, uint32(inheritedGID), inheritedStat.Gid) //nolint:gosec // Test GIDs are non-negative.
	}

	require.NoError(t, secureUpdaterSocket(canonicalSocketPath))
	info, err := os.Lstat(canonicalSocketPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o660), info.Mode().Perm())
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	assert.Equal(t, uint32(os.Geteuid()), stat.Uid) //nolint:gosec // Effective UIDs are non-negative on supported Unix hosts.
	assert.Equal(t, uint32(os.Getegid()), stat.Gid) //nolint:gosec // Effective GIDs are non-negative on supported Unix hosts.
}

func alternateOwnedGroup(t *testing.T) int {
	t.Helper()
	for _, gid := range mustGetgroups(t) {
		if gid != os.Getegid() {
			return gid
		}
	}
	if os.Geteuid() == 0 {
		return os.Getegid() + 1
	}
	return os.Getegid()
}

func mustGetgroups(t *testing.T) []int {
	t.Helper()
	groups, err := os.Getgroups()
	require.NoError(t, err)
	return groups
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	// /tmp keeps the path below macOS's short Unix-domain socket limit.
	directory, err := os.MkdirTemp("/tmp", "proto-fleet-updater-test-") //nolint:usetesting
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(directory)) })
	return filepath.Join(directory, "updater.sock")
}

func TestHandleAcknowledge(t *testing.T) {
	t.Parallel()

	newFailedOperationManager := func(t *testing.T) *Manager {
		t.Helper()
		installRoot := t.TempDir()
		writeCurrentDeployment(t, installRoot, "v1.0.0")
		stateDir := filepath.Join(t.TempDir(), "state")
		writeFailedOperationState(t, stateDir)
		manager, err := NewManager(Config{
			InstallRoot: installRoot,
			StateDir:    stateDir,
			GOARCH:      "amd64",
		})
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, manager.Close()) })
		return manager
	}

	t.Run("acknowledges the current terminal operation", func(t *testing.T) {
		t.Parallel()
		server := NewServer(newFailedOperationManager(t))
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/acknowledge", strings.NewReader(`{"operation_id":"failed-operation"}`))
		server.handleAcknowledge(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code)
		var response updaterapi.AcknowledgeResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		assert.Equal(t, "failed-operation", response.Operation.ID)
		assert.True(t, response.Operation.Acknowledged)
		assert.False(t, response.AlreadyAcknowledged)

		// A retry reports the dismissal as already in place.
		recorder = httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodPost, "/v1/acknowledge", strings.NewReader(`{"operation_id":"failed-operation"}`))
		server.handleAcknowledge(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code)
		response = updaterapi.AcknowledgeResponse{}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		assert.True(t, response.Operation.Acknowledged)
		assert.True(t, response.AlreadyAcknowledged)
	})

	t.Run("unknown operation maps to 404", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/acknowledge", strings.NewReader(`{"operation_id":"other"}`))
		NewServer(newFailedOperationManager(t)).handleAcknowledge(recorder, request)

		assert.Equal(t, http.StatusNotFound, recorder.Code)
	})

	t.Run("active operation maps to 409", func(t *testing.T) {
		t.Parallel()
		manager := newFailedOperationManager(t)
		manager.mu.Lock()
		manager.operation.Phase = updaterapi.PhaseActivating
		manager.mu.Unlock()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/acknowledge", strings.NewReader(`{"operation_id":"failed-operation"}`))
		NewServer(manager).handleAcknowledge(recorder, request)

		assert.Equal(t, http.StatusConflict, recorder.Code)
	})

	t.Run("malformed body maps to 400", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/acknowledge", strings.NewReader(`{"operation_id":"x"}{}`))
		NewServer(&Manager{}).handleAcknowledge(recorder, request)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("empty operation id maps to 400, not 404", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/acknowledge", strings.NewReader(`{}`))
		NewServer(&Manager{}).handleAcknowledge(recorder, request)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("non-POST maps to 405", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/acknowledge", nil)
		NewServer(&Manager{}).handleAcknowledge(recorder, request)

		assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	})
}
