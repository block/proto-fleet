package models

import "time"

const (
	// FleetMetricRollupBucketDuration must match the client dashboard's
	// shortest fleet granularity and the SQL INTERVAL '90 seconds' literals in
	// fleet_metric_rollups.sql.
	FleetMetricRollupBucketDuration = 90 * time.Second
	FleetMetricRollupRawTailBuckets = 2
)

// TruncateToFleetRollupBucket matches TimescaleDB's default time_bucket grid
// for 90 second buckets: the Go zero time and Timescale origin are a whole
// number of days apart, and 86400 is divisible by 90.
func TruncateToFleetRollupBucket(t time.Time) time.Time {
	return t.Truncate(FleetMetricRollupBucketDuration)
}
