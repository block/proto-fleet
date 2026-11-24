package frigateupdate

import "time"

// Config holds configuration for Fleet-triggered Frigate software updates.
type Config struct {
	PackageDir        string        `help:"Directory that stores uploaded Frigate Debian packages. It must be on persistent storage; with the standard deployment the default lives in the persisted firmware volume." default:"firmware/frigate-packages" env:"PACKAGE_DIR"`
	MaxPackageSize    int64         `help:"Maximum Frigate package size in bytes. Frigate itself refuses packages over 1 GiB." default:"536870912" env:"MAX_PACKAGE_SIZE"`
	ReleaseKeysDir    string        `help:"Directory of PEM-encoded Frigate release public keys (ECDSA P-256) used to check package signatures at upload. Controllers re-verify with their own keys. When it is empty, Fleet checks a signature only against a public key supplied with the upload, and accepts an upload it cannot verify with a warning." default:"" env:"RELEASE_KEYS_DIR"`
	PollInterval      time.Duration `help:"How often the update runner advances jobs and reads rollout status from the controllers." default:"5s" env:"POLL_INTERVAL"`
	StageStallTimeout time.Duration `help:"How long package staging may make no progress before the job fails." default:"10m" env:"STAGE_STALL_TIMEOUT"`
	StartTimeout      time.Duration `help:"How long Fleet keeps retrying Start for a job that Frigate does not know before it gives up and fails the job." default:"10m" env:"START_TIMEOUT"`
	CallTimeout       time.Duration `help:"Timeout for one controller update call that does not carry package bytes." default:"30s" env:"CALL_TIMEOUT"`
	ChunkTimeout      time.Duration `help:"Timeout for staging one 1 MiB package chunk." default:"60s" env:"CHUNK_TIMEOUT"`
}

const (
	defaultPollInterval      = 5 * time.Second
	defaultStageStallTimeout = 10 * time.Minute
	defaultStartTimeout      = 10 * time.Minute
	defaultCallTimeout       = 30 * time.Second
	defaultChunkTimeout      = 60 * time.Second
	defaultMaxPackageSize    = int64(512) << 20
)

// withDefaults fills zero values so tests and embedders may leave fields unset.
func (c Config) withDefaults() Config {
	if c.PollInterval <= 0 {
		c.PollInterval = defaultPollInterval
	}
	if c.StageStallTimeout <= 0 {
		c.StageStallTimeout = defaultStageStallTimeout
	}
	if c.StartTimeout <= 0 {
		c.StartTimeout = defaultStartTimeout
	}
	if c.CallTimeout <= 0 {
		c.CallTimeout = defaultCallTimeout
	}
	if c.ChunkTimeout <= 0 {
		c.ChunkTimeout = defaultChunkTimeout
	}
	if c.MaxPackageSize <= 0 {
		c.MaxPackageSize = defaultMaxPackageSize
	}
	return c
}
