package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const (
	Image         = "stratumv2/translator_sv2:main@sha256:70f7edbce640f0e70e44757d700de72ce286da95849b1b2692d250d4d549826d"
	containerName = "proto-fleet-sv2-tproxy"
	componentKey  = "com.protofleet.component"
	imageKey      = "com.protofleet.image"
)

type runtimeState struct {
	Exists  bool
	Running bool
	Managed bool
	Image   string
	Detail  string
}

type containerRuntime interface {
	EnsureStarted(ctx context.Context) error
	State(ctx context.Context) (runtimeState, error)
	Stop(ctx context.Context) error
}

type dockerRuntime struct {
	client  *http.Client
	baseURL string
}

func newDockerRuntime(socketPath string) *dockerRuntime {
	dialer := &net.Dialer{}
	transport := &http.Transport{
		DisableCompression: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &dockerRuntime{
		client:  &http.Client{Transport: transport},
		baseURL: "http://docker/v1.41",
	}
}

func (r *dockerRuntime) EnsureStarted(ctx context.Context) error {
	state, err := r.State(ctx)
	if err != nil {
		return err
	}
	if !state.Exists {
		return fmt.Errorf("SV2 translator container is not prepared; rerun Proto Fleet setup")
	}
	if !state.Managed || state.Image != Image {
		return fmt.Errorf("refusing to start an unrecognized SV2 translator container")
	}
	if state.Running {
		return nil
	}

	response, err := r.do(ctx, http.MethodPost, "/containers/"+containerName+"/start", nil)
	if err != nil {
		return fmt.Errorf("start SV2 translator container: %w", err)
	}
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotModified {
		return dockerResponseError(response, "start SV2 translator container")
	}
	drainAndClose(response.Body)
	return nil
}

func (r *dockerRuntime) State(ctx context.Context) (runtimeState, error) {
	response, err := r.do(ctx, http.MethodGet, "/containers/"+containerName+"/json", nil)
	if err != nil {
		return runtimeState{}, fmt.Errorf("inspect SV2 translator container: %w", err)
	}
	if response.StatusCode == http.StatusNotFound {
		drainAndClose(response.Body)
		return runtimeState{}, nil
	}
	if response.StatusCode != http.StatusOK {
		return runtimeState{}, dockerResponseError(response, "inspect SV2 translator container")
	}
	defer response.Body.Close()

	var inspected struct {
		Config struct {
			Image  string            `json:"Image"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		State struct {
			Running  bool   `json:"Running"`
			Status   string `json:"Status"`
			ExitCode int    `json:"ExitCode"`
			Error    string `json:"Error"`
		} `json:"State"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&inspected); err != nil {
		return runtimeState{}, fmt.Errorf("decode SV2 translator container state: %w", err)
	}

	detail := inspected.State.Status
	if inspected.State.Error != "" {
		detail = inspected.State.Error
	} else if !inspected.State.Running && inspected.State.ExitCode != 0 {
		detail = fmt.Sprintf("container exited with status %d", inspected.State.ExitCode)
	}
	return runtimeState{
		Exists:  true,
		Running: inspected.State.Running,
		Managed: inspected.Config.Labels[componentKey] == "sv2-tproxy" &&
			inspected.Config.Labels[imageKey] == Image,
		Image:  inspected.Config.Image,
		Detail: detail,
	}, nil
}

func (r *dockerRuntime) Stop(ctx context.Context) error {
	state, err := r.State(ctx)
	if err != nil {
		return err
	}
	if !state.Exists || !state.Running {
		return nil
	}
	if !state.Managed || state.Image != Image {
		return fmt.Errorf("refusing to stop an unrecognized SV2 translator container")
	}

	query := url.Values{"t": {"10"}}
	response, err := r.do(ctx, http.MethodPost, "/containers/"+containerName+"/stop", query)
	if err != nil {
		return fmt.Errorf("stop SV2 translator container: %w", err)
	}
	if response.StatusCode != http.StatusNoContent &&
		response.StatusCode != http.StatusNotModified &&
		response.StatusCode != http.StatusNotFound {
		return dockerResponseError(response, "stop SV2 translator container")
	}
	drainAndClose(response.Body)
	return nil
}

func (r *dockerRuntime) do(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
) (*http.Response, error) {
	endpoint := r.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create Docker API request: %w", err)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send Docker API request: %w", err)
	}
	return response, nil
}

func dockerResponseError(response *http.Response, action string) error {
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return fmt.Errorf("%s: Docker API returned HTTP %d", action, response.StatusCode)
	}
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &payload) == nil && strings.TrimSpace(payload.Message) != "" {
		return fmt.Errorf("%s: %s", action, payload.Message)
	}
	return fmt.Errorf("%s: Docker API returned HTTP %d", action, response.StatusCode)
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1024*1024))
	_ = body.Close()
}
