package telemetry

import (
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/scheduler"
)

type Config struct {
	StalenessThreshold time.Duration    `help:"Staleness threshold for telemetry data. If a device's last update is older than this duration, it will be considered stale." default:"1m" env:"STALENESS_THRESHOLD"`
	FetchInterval      time.Duration    `help:"Interval at which to fetch telemetry data from devices." default:"10s" env:"FETCH_INTERVAL"`
	ConcurrencyLimit   int              `help:"Maximum number of concurrent telemetry fetch operations." default:"500" env:"CONCURRENCY_LIMIT"`
	MetricTimeout      time.Duration    `help:"Timeout for telemetry measurements from miners." default:"5s" env:"METRIC_TIMEOUT"`
	SchedulerConfig    scheduler.Config `json:"scheduler_config" yaml:"scheduler_config" envPrefix:"SCHEDULER_"`
}
