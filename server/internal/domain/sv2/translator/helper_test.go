package translator

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type helperTestRuntime struct {
	mu         sync.Mutex
	stateCalls int
	startCalls int
	stopCalls  int
	state      runtimeState
}

func (f *helperTestRuntime) EnsureStarted(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++
	f.state.Exists = true
	f.state.Running = true
	f.state.Managed = true
	f.state.Image = Image
	return nil
}

func (f *helperTestRuntime) State(context.Context) (runtimeState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateCalls++
	return f.state, nil
}

func (f *helperTestRuntime) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	f.state.Running = false
	return nil
}

func TestRuntimeHelperClientAllowsOnlyFixedLifecycleOperations(t *testing.T) {
	// macOS limits Unix socket paths to 104 bytes; t.TempDir is too long there.
	socketDir, err := os.MkdirTemp("/tmp", "sv2-helper-test-") //nolint:usetesting
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socketPath := filepath.Join(socketDir, "runtime.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	runtime := &helperTestRuntime{
		state: runtimeState{
			Exists:  true,
			Managed: true,
			Image:   Image,
		},
	}
	server := &http.Server{
		Handler:           newRuntimeHelperHandler(runtime),
		ReadHeaderTimeout: time.Second,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	})

	client := newHelperRuntime(socketPath)
	state, err := client.State(t.Context())
	require.NoError(t, err)
	assert.False(t, state.Running)

	require.NoError(t, client.EnsureStarted(t.Context()))
	state, err = client.State(t.Context())
	require.NoError(t, err)
	assert.True(t, state.Running)

	require.NoError(t, client.Stop(t.Context()))
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	assert.Equal(t, 1, runtime.startCalls)
	assert.Equal(t, 1, runtime.stopCalls)
}

func TestRuntimeHelperRejectsArbitraryRoutesMethodsAndBodies(t *testing.T) {
	runtime := &helperTestRuntime{}
	handler := newRuntimeHelperHandler(runtime)

	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"http://helper/containers/create",
		http.NoBody,
	)
	require.NoError(t, err)
	response := newRecordingResponse()
	handler.ServeHTTP(response, request)
	assert.Equal(t, http.StatusNotFound, response.status)

	request, err = http.NewRequestWithContext(
		t.Context(),
		http.MethodDelete,
		"http://helper/start",
		http.NoBody,
	)
	require.NoError(t, err)
	response = newRecordingResponse()
	handler.ServeHTTP(response, request)
	assert.Equal(t, http.StatusMethodNotAllowed, response.status)

	request, err = http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"http://helper/start",
		http.NoBody,
	)
	require.NoError(t, err)
	request.URL.RawQuery = "container=other"
	response = newRecordingResponse()
	handler.ServeHTTP(response, request)
	assert.Equal(t, http.StatusBadRequest, response.status)

	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	assert.Equal(t, 0, runtime.startCalls)
	assert.Equal(t, 0, runtime.stopCalls)
}

func TestRemoveStaleSocketRefusesNonSocketPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.sock")
	require.NoError(t, os.WriteFile(path, []byte("do not replace"), 0o600))

	err := removeStaleSocket(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to replace non-socket")
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "do not replace", string(data))
}

type recordingResponse struct {
	header http.Header
	status int
}

func newRecordingResponse() *recordingResponse {
	return &recordingResponse{header: make(http.Header), status: http.StatusOK}
}

func (r *recordingResponse) Header() http.Header {
	return r.header
}

func (r *recordingResponse) Write(data []byte) (int, error) {
	return len(data), nil
}

func (r *recordingResponse) WriteHeader(statusCode int) {
	r.status = statusCode
}
