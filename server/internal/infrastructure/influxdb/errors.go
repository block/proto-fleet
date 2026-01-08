package influxdb

import (
	"fmt"
	"net"
	"strings"
	"syscall"
)

type InfluxTelemetryError struct {
	Operation    string
	ErrorType    TelemetryErrorType
	Cause        error
	Context      map[string]interface{}
	PartialData  bool
	ErrorCount   int
	RetryAttempt int
}

type TelemetryErrorType int

// TelemetryErrorType constants define different types of telemetry errors
const (
	TelemetryErrorTypeUnknown TelemetryErrorType = iota
	TelemetryErrorTypeConnection
	TelemetryErrorTypeConfig
	TelemetryErrorTypeQuery
	TelemetryErrorTypeWrite
	TelemetryErrorTypeIteration
	TelemetryErrorTypeDataConversion
	TelemetryErrorTypeClose
	TelemetryErrorTypePing
)

func (t TelemetryErrorType) String() string {
	switch t {
	case TelemetryErrorTypeUnknown:
		return "unknown"
	case TelemetryErrorTypeConnection:
		return "connection"
	case TelemetryErrorTypeConfig:
		return "config"
	case TelemetryErrorTypeQuery:
		return "query"
	case TelemetryErrorTypeWrite:
		return "write"
	case TelemetryErrorTypeIteration:
		return "iteration"
	case TelemetryErrorTypeDataConversion:
		return "data_conversion"
	case TelemetryErrorTypeClose:
		return "close"
	case TelemetryErrorTypePing:
		return "ping"
	default:
		return "unknown"
	}
}

func (e *InfluxTelemetryError) Error() string {
	retryInfo := ""
	if e.RetryAttempt > 0 {
		retryInfo = fmt.Sprintf(" (after %d retries)", e.RetryAttempt)
	}

	if e.PartialData {
		return fmt.Sprintf("influx telemetry %s error (partial data)%s: %v", e.ErrorType.String(), retryInfo, e.Cause)
	}
	if e.ErrorCount > 1 {
		return fmt.Sprintf("influx telemetry %s error (%d errors)%s: %v", e.ErrorType.String(), e.ErrorCount, retryInfo, e.Cause)
	}
	return fmt.Sprintf("influx telemetry %s error%s: %v", e.ErrorType.String(), retryInfo, e.Cause)
}

func (e *InfluxTelemetryError) Unwrap() error {
	return e.Cause
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout() || netErr.Temporary()
	}

	if opErr, ok := err.(*net.OpError); ok {
		_ = opErr
		return true
	}

	if err == syscall.ECONNREFUSED || err == syscall.ECONNRESET || err == syscall.ETIMEDOUT {
		return true
	}

	errStr := strings.ToLower(err.Error())
	retryableStrings := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"temporary failure",
		"service unavailable",
		"too many requests",
		"rate limit",
		"server error",
		"internal server error",
		"bad gateway",
		"gateway timeout",
		"network is unreachable",
		"no route to host",
	}

	for _, retryableStr := range retryableStrings {
		if strings.Contains(errStr, retryableStr) {
			return true
		}
	}

	return false
}

// Error constructors for InfluxTelemetryError

func newTelemetryWriteError(err error, pointCount int) error {
	return &InfluxTelemetryError{
		Operation: "Store",
		ErrorType: TelemetryErrorTypeWrite,
		Cause:     err,
		Context: map[string]interface{}{
			"point_count": pointCount,
		},
	}
}

func newTelemetryWriteErrorWithRetry(err error, pointCount int, retryAttempt int) error {
	return &InfluxTelemetryError{
		Operation:    "Store",
		ErrorType:    TelemetryErrorTypeWrite,
		Cause:        err,
		RetryAttempt: retryAttempt,
		Context: map[string]interface{}{
			"point_count":   pointCount,
			"retry_attempt": retryAttempt,
		},
	}
}

func newTelemetryQueryError(err error, queryType string) error {
	return &InfluxTelemetryError{
		Operation: queryType,
		ErrorType: TelemetryErrorTypeQuery,
		Cause:     err,
		Context: map[string]interface{}{
			"query_type": queryType,
		},
	}
}

func newTelemetryConnectionError(err error) error {
	return &InfluxTelemetryError{
		Operation: "Connect",
		ErrorType: TelemetryErrorTypeConnection,
		Cause:     err,
		Context:   map[string]interface{}{},
	}
}

func newTelemetryConfigError(err error) error {
	return &InfluxTelemetryError{
		Operation: "Configure",
		ErrorType: TelemetryErrorTypeConfig,
		Cause:     err,
		Context:   map[string]interface{}{},
	}
}

func newTelemetryIterationError(err error, operation string, errorCount int, hasPartialData bool) error {
	return &InfluxTelemetryError{
		Operation:   operation,
		ErrorType:   TelemetryErrorTypeIteration,
		Cause:       err,
		PartialData: hasPartialData,
		ErrorCount:  errorCount,
		Context: map[string]interface{}{
			"operation":        operation,
			"error_count":      errorCount,
			"has_partial_data": hasPartialData,
		},
	}
}

func newTelemetryCloseError(err error) error {
	return &InfluxTelemetryError{
		Operation: "Close",
		ErrorType: TelemetryErrorTypeClose,
		Cause:     err,
		Context:   map[string]interface{}{},
	}
}

func newTelemetryPingError(err error) error {
	return &InfluxTelemetryError{
		Operation: "Ping",
		ErrorType: TelemetryErrorTypePing,
		Cause:     err,
		Context:   map[string]interface{}{},
	}
}

// isTableNotFoundError checks if the error indicates a table doesn't exist in InfluxDB.
// This is expected when no data has been written yet (e.g., fresh database with no miners paired).
func isTableNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "table") && strings.Contains(errStr, "not found")
}
