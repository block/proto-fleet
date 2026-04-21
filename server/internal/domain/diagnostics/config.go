package diagnostics

import "time"

// Config contains configuration for the diagnostics service.
type Config struct {
	WatchPollInterval  time.Duration `help:"Interval between Watch RPC database polls" default:"20s" env:"WATCH_POLL_INTERVAL"`
	WatchChannelBuffer int           `help:"Buffer size for Watch RPC update channels" default:"10" env:"WATCH_CHANNEL_BUFFER"`

	CloserPollInterval       time.Duration `help:"Interval between stale error closing polls" default:"30s" env:"CLOSER_POLL_INTERVAL"`
	CloserStalenessThreshold time.Duration `help:"Duration after which unseen errors are closed" default:"2m" env:"CLOSER_STALENESS_THRESHOLD"`
}

// getConfigDurationOrDefault returns the configured duration value, or the default if the value is zero or negative.
// This helper centralizes the fallback logic used throughout the diagnostics service for duration configuration.
func getConfigDurationOrDefault(configValue, defaultValue time.Duration) time.Duration {
	if configValue <= 0 {
		return defaultValue
	}
	return configValue
}

// getConfigIntOrDefault returns the configured int value, or the default if the value is zero or negative.
// This helper centralizes the fallback logic used throughout the diagnostics service for int configuration.
func getConfigIntOrDefault(configValue, defaultValue int) int {
	if configValue <= 0 {
		return defaultValue
	}
	return configValue
}
