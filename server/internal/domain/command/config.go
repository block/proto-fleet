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

	// Reconciler backfills the activity '<event_type>.completed' row for any
	// batch that FINISHED without one (e.g. due to a server crash or a
	// finalizer that exhausted its retries). See reconciler.go for details.
	ReconcilerInterval    time.Duration `help:"How often the completion reconciler runs." default:"5m" env:"RECONCILER_INTERVAL"`
	ReconcilerGracePeriod time.Duration `help:"How long to wait after a batch FINISHED before the reconciler treats it as missing its completion row." default:"2m" env:"RECONCILER_GRACE_PERIOD"`
	ReconcilerMaxBatches  int           `help:"Maximum batches the reconciler backfills per tick." default:"200" env:"RECONCILER_MAX_BATCHES"`

	// Retention governs paginated cleanup of the command-audit tables.
	// Defaults are balanced for typical fleets; operators can tune per env.
	Retention RetentionConfig `embed:"" prefix:""`
}

// RetentionConfig controls the command retention cleaner. Delete order is
// enforced by the cleaner to respect FK constraints: queue_message terminal
// rows first, then command_on_device_log, then command_batch_log headers.
type RetentionConfig struct {
	QueueMessageRetention time.Duration `help:"Retain SUCCESS/FAILED queue_message rows for this long before the cleaner deletes them." default:"720h" env:"QUEUE_MESSAGE_RETENTION"`
	DeviceLogRetention    time.Duration `help:"Retain command_on_device_log rows for this long." default:"2160h" env:"DEVICE_LOG_RETENTION"`
	BatchLogRetention     time.Duration `help:"Retain command_batch_log headers for this long after finishing." default:"4320h" env:"BATCH_LOG_RETENTION"`
	CleanupInterval       time.Duration `help:"How often the command retention cleaner runs." default:"1h" env:"COMMAND_CLEANUP_INTERVAL"`
	DeleteBatchLimit      int           `help:"Maximum rows deleted per retention query; the cleaner loops until the query returns zero." default:"1000" env:"COMMAND_DELETE_BATCH_LIMIT"`
}
