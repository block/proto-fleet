package updates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/internal/updaterapi"
)

var errExecutorUnavailable = errors.New("host updater unavailable")

type executorClient interface {
	Status(ctx context.Context) (updaterapi.StatusResponse, error)
	Trigger(ctx context.Context, targetVersion string) (updaterapi.Operation, error)
}

type unixExecutorClient struct {
	http *http.Client
}

type executorHTTPError struct {
	StatusCode int
	Message    string
}

func (e *executorHTTPError) Error() string { return e.Message }

func newExecutorClient(socketPath string) executorClient {
	if socketPath == "" {
		return nil
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 2 * time.Second}
			connection, err := dialer.DialContext(ctx, "unix", socketPath)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", errExecutorUnavailable, err)
			}
			return connection, nil
		},
	}
	return &unixExecutorClient{http: &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}}
}

func (c *unixExecutorClient) Status(ctx context.Context) (updaterapi.StatusResponse, error) {
	var response updaterapi.StatusResponse
	if err := c.do(ctx, http.MethodGet, "/v1/status", nil, &response); err != nil {
		return updaterapi.StatusResponse{}, err
	}
	return response, nil
}

func (c *unixExecutorClient) Trigger(ctx context.Context, targetVersion string) (updaterapi.Operation, error) {
	request := updaterapi.TriggerRequest{TargetVersion: targetVersion}
	var response updaterapi.TriggerResponse
	if err := c.do(ctx, http.MethodPost, "/v1/upgrade", request, &response); err != nil {
		return updaterapi.Operation{}, err
	}
	return response.Operation, nil
}

func (c *unixExecutorClient) do(ctx context.Context, method, path string, body, output any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode host updater request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://updater"+path, reader)
	if err != nil {
		return fmt.Errorf("create host updater request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		if errors.Is(err, errExecutorUnavailable) || strings.Contains(err.Error(), "connect: no such file or directory") {
			return errExecutorUnavailable
		}
		return fmt.Errorf("call host updater: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure updaterapi.ErrorResponse
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&failure)
		if failure.Error == "" {
			failure.Error = response.Status
		}
		return &executorHTTPError{StatusCode: response.StatusCode, Message: failure.Error}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output); err != nil {
		return fmt.Errorf("decode host updater response: %w", err)
	}
	return nil
}
