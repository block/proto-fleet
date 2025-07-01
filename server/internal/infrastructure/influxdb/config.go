package influxdb

import (
	"fmt"
	"net/url"
	"time"
)

type Config struct {
	URL          string `json:"url" validate:"required,url" env:"INFLUX_URL"`
	Organization string `json:"organization" validate:"required" env:"INFLUX_ORG"`
	Bucket       string `json:"bucket" validate:"required" env:"INFLUX_BUCKET"`
	Token        string `json:"token" validate:"required" env:"INFLUX_TOKEN"`

	WriteTimeout  time.Duration `json:"write_timeout" default:"30s"`
	QueryTimeout  time.Duration `json:"query_timeout" default:"60s"`
	BatchSize     int           `json:"batch_size" default:"1000"`
	FlushInterval time.Duration `json:"flush_interval" default:"5s"`
	RetryAttempts int           `json:"retry_attempts" default:"3"`
	RetryDelay    time.Duration `json:"retry_delay" default:"100ms"`
}

func validateConfig(config Config) error {
	if config.URL == "" {
		return fmt.Errorf("URL is required")
	}

	if _, err := url.Parse(config.URL); err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	if config.Organization == "" {
		return fmt.Errorf("organization is required")
	}

	if config.Bucket == "" {
		return fmt.Errorf("bucket is required")
	}

	if config.Token == "" {
		return fmt.Errorf("token is required")
	}
	return nil
}
