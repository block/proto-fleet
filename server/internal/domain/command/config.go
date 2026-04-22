package command

import "time"

type Config struct {
	MaxWorkers                       int32         `help:"Max number of worker threads running at the same time." default:"500" env:"MAX_WORKERS"`
	MasterPollingInterval            time.Duration `help:"Interval in which the master polls the batch status check." default:"1s" env:"MASTER_POLLING_INTERVAL"`
	WorkerExecutionTimeout           time.Duration `help:"Limit for a single worker thread runtime." default:"30s" env:"WORKER_EXECUTION_TIMEOUT"`
	BatchStatusUpdatePollingInterval time.Duration `help:"Interval in which the start and finish for batch is polled." default:"1s" env:"BATCH_STATUS_UPDATE_POLLING_INTERVAL"`
	DequeueRetries                   int           `help:"Number of retries to dequeue messages from the queue." default:"10" env:"DEQUEUE_RETRIES"`
	StuckMessageTimeout              time.Duration `help:"How long a PROCESSING message can be idle before the reaper marks it FAILED." default:"5m" env:"STUCK_MESSAGE_TIMEOUT"`
	ReaperInterval                   time.Duration `help:"How often the stuck message reaper runs." default:"30s" env:"REAPER_INTERVAL"`
	FirmwareUpdateTimeout            time.Duration `help:"Timeout for firmware update workers including install polling." default:"15m" env:"FIRMWARE_UPDATE_TIMEOUT"`
	FirmwareUpdateStuckTimeout       time.Duration `help:"How long a firmware update PROCESSING message can be idle before the reaper marks it FAILED." default:"20m" env:"FIRMWARE_UPDATE_STUCK_TIMEOUT"`
}
