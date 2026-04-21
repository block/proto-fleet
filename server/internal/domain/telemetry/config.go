package telemetry

import (
	"time"
)

type Config struct {
	StalenessThreshold       time.Duration `help:"Staleness threshold for telemetry data. If a device's last update is older than this duration, it will be considered stale." default:"1m" env:"STALENESS_THRESHOLD"`
	FetchInterval            time.Duration `help:"Interval at which to fetch telemetry data from devices." default:"10s" env:"FETCH_INTERVAL"`
	ConcurrencyLimit         int           `help:"Maximum number of concurrent telemetry fetch operations." default:"500" env:"CONCURRENCY_LIMIT"`
	MetricTimeout            time.Duration `help:"Timeout for telemetry measurements from miners." default:"5s" env:"METRIC_TIMEOUT"`
	DevicePollInterval       time.Duration `help:"Interval at which to poll for new paired devices." default:"10m" env:"DEVICE_POLL_INTERVAL"`
	NewDeviceLookback        time.Duration `help:"Lookback period for new devices to consider for telemetry." default:"10m" env:"NEW_DEVICE_LOOKBACK"`
	DeviceStatusPollInterval time.Duration `help:"Interval at which to poll for device status updates." default:"10s" env:"DEVICE_STATUS_POLL_INTERVAL"`
	StatusFlushInterval      time.Duration `help:"Interval at which to flush accumulated status updates to DB. Longer intervals batch more updates together." default:"1s" env:"STATUS_FLUSH_INTERVAL"`
	StateSnapshotInterval    time.Duration `help:"Interval at which to write per-device miner state snapshots powering the uptime chart." default:"60s" env:"STATE_SNAPSHOT_INTERVAL"`
}
