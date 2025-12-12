package diagnostics

import "time"

// Config contains configuration for the diagnostics service.
type Config struct {
	WatchPollInterval  time.Duration `help:"Interval between Watch RPC database polls" default:"20s" env:"WATCH_POLL_INTERVAL"`
	WatchChannelBuffer int           `help:"Buffer size for Watch RPC update channels" default:"10" env:"WATCH_CHANNEL_BUFFER"`
}
