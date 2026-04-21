package scheduler

type Config struct {
	MaxConsecutiveFailures int `help:"Maximum number of consecutive failures before a device is considered stale." default:"20" env:"MAX_CONSECUTIVE_FAILURES" json:"max_consecutive_failures"`
}
