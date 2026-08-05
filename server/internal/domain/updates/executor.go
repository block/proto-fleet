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
	"time"

	"github.com/block/proto-fleet/server/internal/updaterapi"
)

const (
	executorDialTimeout             = 2 * time.Second
	executorHTTPTimeout             = 5 * time.Second
	maxExecutorErrorResponseBytes   = int64(64 << 10)
	maxExecutorSuccessResponseBytes = int64(1 << 20)
)

var (
	errExecutorUnavailable      = errors.New("host updater unavailable")
	errExecutorResponseTooLarge = errors.New("host updater response exceeds size limit")
)

type executorClient interface {
	Status(ctx context.Context) (updaterapi.StatusResponse, error)
	Trigger(ctx context.Context, operationID, targetVersion string) (updaterapi.Operation, error)
}

type unixExecutorClient struct {
	http *http.Client
}

type executorHTTPError struct {
	StatusCode int
	Message    string
}

func (e *executorHTTPError) Error() string { return e.Message }

// executorTransportError means the request may have crossed the socket but no
// definitive HTTP response was received. Mutations carrying this error must be
// reconciled against durable updater status before Fleet reports failure.
type executorTransportError struct{ cause error }

func (e *executorTransportError) Error() string { return fmt.Sprintf("call host updater: %v", e.cause) }
func (e *executorTransportError) Unwrap() error { return e.cause }

// executorProtocolError means a successful HTTP response could not be trusted
// (malformed, oversized, or inconsistent). The mutation may still have been
// accepted, so callers reconcile it just like a lost transport response, but
// report an internal protocol failure when reconciliation cannot prove that.
type executorProtocolError struct{ cause error }

func (e *executorProtocolError) Error() string {
	return fmt.Sprintf("decode host updater response: %v", e.cause)
}
func (e *executorProtocolError) Unwrap() error { return e.cause }

func newExecutorClient(socketPath string) executorClient {
	if socketPath == "" {
		return nil
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: executorDialTimeout}
			connection, err := dialer.DialContext(ctx, "unix", socketPath)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", errExecutorUnavailable, err)
			}
			return connection, nil
		},
	}
	return &unixExecutorClient{http: &http.Client{
		Transport: transport,
		Timeout:   executorHTTPTimeout,
	}}
}

func (c *unixExecutorClient) Status(ctx context.Context) (updaterapi.StatusResponse, error) {
	var response updaterapi.StatusResponse
	if err := c.do(ctx, http.MethodGet, "/v1/status", nil, &response); err != nil {
		return updaterapi.StatusResponse{}, err
	}
	return response, nil
}

func (c *unixExecutorClient) Trigger(ctx context.Context, operationID, targetVersion string) (updaterapi.Operation, error) {
	request := updaterapi.TriggerRequest{OperationID: operationID, TargetVersion: targetVersion}
	var response updaterapi.TriggerResponse
	if err := c.do(ctx, http.MethodPost, "/v1/upgrade", request, &response); err != nil {
		return updaterapi.Operation{}, err
	}
	if response.Operation.ID != operationID || response.Operation.TargetVersion != targetVersion {
		return updaterapi.Operation{}, &executorProtocolError{cause: fmt.Errorf(
			"operation identity mismatch: got id %q target %q",
			response.Operation.ID,
			response.Operation.TargetVersion,
		)}
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
		if ctxErr := ctx.Err(); ctxErr != nil {
			return &executorTransportError{cause: ctxErr}
		}
		if errors.Is(err, errExecutorUnavailable) {
			return errExecutorUnavailable
		}
		return &executorTransportError{cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure updaterapi.ErrorResponse
		data, readErr := readExecutorBody(response.Body, maxExecutorErrorResponseBytes)
		if readErr == nil {
			_ = decodeExecutorJSON(data, &failure)
		}
		if failure.Error == "" {
			failure.Error = response.Status
		}
		return &executorHTTPError{StatusCode: response.StatusCode, Message: failure.Error}
	}
	data, err := readExecutorBody(response.Body, maxExecutorSuccessResponseBytes)
	if err != nil {
		if errors.Is(err, errExecutorResponseTooLarge) {
			return &executorProtocolError{cause: err}
		}
		return &executorTransportError{cause: err}
	}
	if err := decodeExecutorJSON(data, output); err != nil {
		return &executorProtocolError{cause: err}
	}
	return nil
}

func readExecutorBody(body io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read host updater response: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w (%d bytes)", errExecutorResponseTooLarge, maxBytes)
	}
	return data, nil
}

func decodeExecutorJSON(data []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("response body must contain exactly one JSON value")
		}
		return fmt.Errorf("decode trailing response body: %w", err)
	}
	return nil
}
