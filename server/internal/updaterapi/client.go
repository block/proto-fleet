package updaterapi

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
)

const (
	dialTimeout             = 2 * time.Second
	httpTimeout             = 5 * time.Second
	maxErrorResponseBytes   = int64(64 << 10)
	maxSuccessResponseBytes = int64(1 << 20)
)

var ErrUnavailable = errors.New("host updater unavailable")

type HTTPError struct {
	StatusCode int
	Message    string
	Code       ErrorCode
}

func (e *HTTPError) Error() string { return e.Message }

type TransportError struct{ Cause error }

func (e *TransportError) Error() string { return fmt.Sprintf("call host updater: %v", e.Cause) }
func (e *TransportError) Unwrap() error { return e.Cause }

type ProtocolError struct{ Cause error }

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("decode host updater response: %v", e.Cause)
}
func (e *ProtocolError) Unwrap() error { return e.Cause }

type Client struct {
	http *http.Client
}

func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: dialTimeout}
			connection, err := dialer.DialContext(ctx, "unix", socketPath)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
			}
			return connection, nil
		},
	}
	return NewClientWithHTTP(&http.Client{Transport: transport, Timeout: httpTimeout})
}

// NewClientWithHTTP supports callers that already own the local transport and focused transport tests.
func NewClientWithHTTP(client *http.Client) *Client { return &Client{http: client} }

func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

func (c *Client) Status(ctx context.Context) (StatusResponse, error) {
	var response StatusResponse
	if err := c.do(ctx, http.MethodGet, "/v1/status", nil, &response); err != nil {
		return StatusResponse{}, err
	}
	return response, nil
}

func (c *Client) Trigger(ctx context.Context, operationID, targetVersion string) (Operation, error) {
	return c.trigger(ctx, operationID, targetVersion, false)
}

func (c *Client) TriggerComplete(ctx context.Context, operationID, targetVersion string) (Operation, error) {
	return c.trigger(ctx, operationID, targetVersion, true)
}

func (c *Client) Acknowledge(ctx context.Context, operationID string, expectedOutcomeRevision uint64) (Operation, bool, error) {
	request := AcknowledgeRequest{
		OperationID:             operationID,
		ExpectedOutcomeRevision: expectedOutcomeRevision,
	}
	var response AcknowledgeResponse
	if err := c.do(ctx, http.MethodPost, "/v1/acknowledge", request, &response); err != nil {
		return Operation{}, false, err
	}
	if response.Operation.ID != operationID || response.Operation.OutcomeRevision != expectedOutcomeRevision {
		return Operation{}, false, &ProtocolError{Cause: fmt.Errorf(
			"operation outcome identity mismatch: got id %q revision %d",
			response.Operation.ID,
			response.Operation.OutcomeRevision,
		)}
	}
	return response.Operation, response.AlreadyAcknowledged, nil
}

func (c *Client) trigger(ctx context.Context, operationID, targetVersion string, complete bool) (Operation, error) {
	request := TriggerRequest{OperationID: operationID, TargetVersion: targetVersion, Complete: complete}
	var response TriggerResponse
	if err := c.do(ctx, http.MethodPost, "/v1/upgrade", request, &response); err != nil {
		return Operation{}, err
	}
	if response.Operation.ID != operationID || response.Operation.TargetVersion != targetVersion || response.Operation.Complete != complete {
		return Operation{}, &ProtocolError{Cause: fmt.Errorf(
			"operation identity mismatch: got id %q target %q complete %t",
			response.Operation.ID,
			response.Operation.TargetVersion,
			response.Operation.Complete,
		)}
	}
	return response.Operation, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, output any) error {
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
			return &TransportError{Cause: ctxErr}
		}
		if errors.Is(err, ErrUnavailable) {
			return ErrUnavailable
		}
		return &TransportError{Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var failure ErrorResponse
		data, readErr := readBody(response.Body, maxErrorResponseBytes)
		if readErr == nil {
			if decodeErr := decodeJSON(data, &failure); decodeErr != nil {
				failure = ErrorResponse{}
			}
		}
		if failure.Error == "" {
			failure.Error = response.Status
		}
		return &HTTPError{StatusCode: response.StatusCode, Message: failure.Error, Code: failure.Code}
	}
	data, err := readBody(response.Body, maxSuccessResponseBytes)
	if err != nil {
		return &ProtocolError{Cause: err}
	}
	if err := decodeJSON(data, output); err != nil {
		return &ProtocolError{Cause: err}
	}
	return nil
}

func readBody(body io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read host updater response: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("host updater response exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func decodeJSON(data []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("response body must contain exactly one JSON value")
		}
		return fmt.Errorf("decode trailing response body: %w", err)
	}
	return nil
}
