package hostedaccounts

import (
	"crypto/subtle"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// verifyStoredPools consumes a complete stored-configuration read, never runtime
// pool telemetry. Slots beyond the desired list must be explicitly empty; every
// credential must be observed rather than inferred from a successful write.
func verifyStoredPools(expected []sdk.MiningPoolConfig, actual []interfaces.MinerStoredPool) error {
	mismatch := func() error {
		return fleeterror.NewFailedPreconditionError("stored pool configuration could not be verified")
	}
	if len(expected) < 1 || len(expected) > 3 || len(actual) < len(expected) || len(actual) > 3 {
		return mismatch()
	}
	seen := [3]bool{}
	for _, pool := range actual {
		if pool.Priority < 0 || int(pool.Priority) >= len(actual) || seen[pool.Priority] || pool.Password == nil {
			return mismatch()
		}
		seen[pool.Priority] = true
		i := int(pool.Priority)
		if i >= len(expected) {
			if pool.URL != "" || pool.Username != "" || *pool.Password != "" {
				return mismatch()
			}
			continue
		}
		desired := expected[i]
		if desired.Priority != pool.Priority || !validPoolURL(desired.URL) || desired.WorkerName == "" ||
			pool.URL != desired.URL || pool.Username != desired.WorkerName ||
			subtle.ConstantTimeCompare([]byte(*pool.Password), []byte(desired.Password)) != 1 {
			return mismatch()
		}
	}
	return nil
}
