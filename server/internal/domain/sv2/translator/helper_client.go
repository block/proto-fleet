package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type helperRuntime struct {
	client  *http.Client
	baseURL string
}

func newHelperRuntime(socketPath string) *helperRuntime {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DisableCompression: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &helperRuntime{
		client: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
		baseURL: "http://sv2-runtime-helper",
	}
}

func (r *helperRuntime) EnsureStarted(ctx context.Context) error {
	state, err := r.requestState(ctx, http.MethodPost, "/start")
	if err != nil {
		return fmt.Errorf("start SV2 translator through lifecycle helper: %w", err)
	}
	if !state.Exists || !state.Managed || state.Image != Image || !state.Running {
		return fmt.Errorf("SV2 translator lifecycle helper returned an invalid running state")
	}
	return nil
}

func (r *helperRuntime) State(ctx context.Context) (runtimeState, error) {
	state, err := r.requestState(ctx, http.MethodGet, "/state")
	if err != nil {
		return runtimeState{}, fmt.Errorf("inspect SV2 translator through lifecycle helper: %w", err)
	}
	return state, nil
}

func (r *helperRuntime) Stop(ctx context.Context) error {
	state, err := r.requestState(ctx, http.MethodPost, "/stop")
	if err != nil {
		return fmt.Errorf("stop SV2 translator through lifecycle helper: %w", err)
	}
	if state.Running {
		return fmt.Errorf("SV2 translator lifecycle helper reported the container still running")
	}
	return nil
}

func (r *helperRuntime) requestState(
	ctx context.Context,
	method string,
	path string,
) (runtimeState, error) {
	request, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, http.NoBody)
	if err != nil {
		return runtimeState{}, fmt.Errorf("create lifecycle helper request: %w", err)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return runtimeState{}, fmt.Errorf("send lifecycle helper request: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return runtimeState{}, helperResponseError(response)
	}
	defer response.Body.Close()

	var state runtimeState
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&state); err != nil {
		return runtimeState{}, fmt.Errorf("decode lifecycle helper response: %w", err)
	}
	return state, nil
}

func helperResponseError(response *http.Response) error {
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("lifecycle helper returned HTTP %d", response.StatusCode)
	}
	message := strings.TrimSpace(string(data))
	if message == "" {
		return fmt.Errorf("lifecycle helper returned HTTP %d", response.StatusCode)
	}
	return fmt.Errorf("lifecycle helper returned HTTP %d: %s", response.StatusCode, message)
}
