package interceptors

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"time"

	"connectrpc.com/connect"
)

const (
	maxRetries = 3
	baseDelay  = 100 * time.Millisecond
	maxDelay   = 5 * time.Second
)

// RetryInterceptor handles retry logic with exponential backoff
type RetryInterceptor struct{}

// NewRetryInterceptor creates a new retry interceptor
func NewRetryInterceptor() connect.Interceptor {
	return &RetryInterceptor{}
}

// WrapUnary implements the connect.Interceptor interface
func (i *RetryInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		var lastErr error
		//nolint:intrange // Use a loop to retry the request, we want a uint for attempts
		for attempt := uint(0); attempt < maxRetries; attempt++ {
			resp, err := next(ctx, req)
			if err == nil {
				return resp, nil
			}

			lastErr = err
			if !isRetryableError(err) {
				break
			}

			// Don't retry on the last attempt
			if attempt == maxRetries-1 {
				break
			}

			// Exponential backoff with jitter
			backoff := calculateBackoff(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				continue
			}
		}
		return nil, lastErr
	}
}

// WrapStreamingClient implements the connect.Interceptor interface
func (i *RetryInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		// For streaming, we don't retry - that would be handled at a higher level
		return next(ctx, spec)
	}
}

// WrapStreamingHandler implements the connect.Interceptor interface
func (i *RetryInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next // No modification needed for server-side handlers
}

// isRetryableError determines if an error should trigger a retry
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Retry on temporary network errors
	retryableErrors := []string{
		"connection refused",
		"timeout",
		"temporary failure",
		"network is unreachable",
		"connection reset",
		"broken pipe",
	}

	for _, retryable := range retryableErrors {
		if strings.Contains(errStr, retryable) {
			return true
		}
	}

	// Check Connect error codes
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		//nolint:exhaustive // Handle specific Connect error codes that indicate retryable conditions
		switch connectErr.Code() {
		case connect.CodeUnavailable,
			connect.CodeDeadlineExceeded,
			connect.CodeAborted,
			connect.CodeInternal:
			return true
		}
	}

	return false
}

// calculateBackoff calculates the backoff duration with jitter
func calculateBackoff(attempt uint) time.Duration {
	// Exponential backoff: baseDelay * 2^attempt
	delay := baseDelay * time.Duration(1<<attempt)

	// Cap the delay
	if delay > maxDelay {
		delay = maxDelay
	}

	// Add jitter (±25%)
	jitter := time.Duration(rand.Int63n(int64(delay / 2)))
	delay = delay + jitter - delay/4

	return delay
}
