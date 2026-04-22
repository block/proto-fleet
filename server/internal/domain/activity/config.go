package activity

import "time"

// Config groups activity-domain settings. Retention governs how long rows in
// activity_log are retained before the cleaner ages them out.
//
// Defaults are balanced for typical fleets; operators can tune per env.
type Config struct {
	Retention RetentionConfig `embed:"" prefix:""`
}

// RetentionConfig controls the activity retention cleaner. See retention.go.
type RetentionConfig struct {
	ActivityLogRetention time.Duration `help:"Retain activity_log rows for this long before the cleaner deletes them." default:"8760h" env:"ACTIVITY_LOG_RETENTION"`
	CleanupInterval      time.Duration `help:"How often the activity retention cleaner runs." default:"6h" env:"ACTIVITY_CLEANUP_INTERVAL"`
	DeleteBatchLimit     int           `help:"Maximum rows deleted per retention query; the cleaner loops until the query returns zero." default:"1000" env:"ACTIVITY_DELETE_BATCH_LIMIT"`
}
