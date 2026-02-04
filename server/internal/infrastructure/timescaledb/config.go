package timescaledb

import "time"

const (
	// DefaultBucketDuration is the default time bucket for metric aggregation.
	DefaultBucketDuration = 10 * time.Second
)

// Config holds configuration for the TimescaleDB telemetry store.
type Config struct {
	// MaxAge is the maximum age of metrics to query by default
	MaxAge time.Duration `json:"max_age" default:"24h" env:"MAX_AGE"`

	// PollInterval is the interval for polling updates when streaming
	PollInterval time.Duration `json:"poll_interval" default:"1s" env:"POLL_INTERVAL"`

	// BufferSize is the size of the channel buffer for streaming
	BufferSize int `json:"buffer_size" default:"100" env:"BUFFER_SIZE"`

	// WriteTimeout is the timeout for write operations
	WriteTimeout time.Duration `json:"write_timeout" default:"30s" env:"WRITE_TIMEOUT"`

	// QueryTimeout is the timeout for query operations
	QueryTimeout time.Duration `json:"query_timeout" default:"60s" env:"QUERY_TIMEOUT"`

	// MaxTimeSeriesRows is the maximum number of rows to return from time series queries
	MaxTimeSeriesRows int `json:"max_time_series_rows" default:"100000" env:"MAX_TIME_SERIES_ROWS"`
}

// DefaultConfig returns the default configuration for the TimescaleDB store.
func DefaultConfig() Config {
	return Config{
		MaxAge:            24 * time.Hour,
		PollInterval:      1 * time.Second,
		BufferSize:        100,
		WriteTimeout:      30 * time.Second,
		QueryTimeout:      60 * time.Second,
		MaxTimeSeriesRows: 100000,
	}
}
