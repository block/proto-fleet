package workername

import (
	"context"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

type PoolSyncStatus string

const (
	PoolSyncStatusPoolUpdatedSuccessfully PoolSyncStatus = "POOL_UPDATED_SUCCESSFULLY"
)

func FromPoolUsername(username string) string {
	trimmed := strings.TrimSpace(username)
	firstSeparator := strings.Index(trimmed, ".")
	if firstSeparator <= 0 || firstSeparator == len(trimmed)-1 {
		return ""
	}

	return strings.TrimSpace(trimmed[firstSeparator+1:])
}

// EffectivePoolUsername applies the same worker suffix rules used by pool
// command execution. MinerChannel normalization uses this to compare desired state.
func EffectivePoolUsername(username, workerName string, appendWorkerName bool) string {
	trimmed := strings.TrimSpace(username)
	if !appendWorkerName || workerName == "" || trimmed == "" || strings.Contains(trimmed, ".") {
		return trimmed
	}
	return normalizedPoolUsernameBase(trimmed) + "." + workerName
}

// RewritePoolUsername replaces an existing worker suffix with the stored name.
func RewritePoolUsername(username, workerName string) string {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" || workerName == "" {
		return trimmed
	}
	base := normalizedPoolUsernameBase(trimmed)
	if base == "" {
		return trimmed
	}
	return base + "." + workerName
}

func normalizedPoolUsernameBase(username string) string {
	trimmed := strings.TrimSpace(username)
	firstSeparator := strings.Index(trimmed, ".")
	if firstSeparator <= 0 || firstSeparator == len(trimmed)-1 {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:firstSeparator])
}

func HasStored(
	ctx context.Context,
	deviceStore interfaces.DeviceStore,
	orgID int64,
	deviceIdentifier string,
) (bool, error) {
	props, err := deviceStore.GetDevicePropertiesForRename(ctx, orgID, []string{deviceIdentifier}, false)
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to read existing worker name: %v", err)
	}
	if len(props) == 0 {
		return false, nil
	}

	return strings.TrimSpace(props[0].WorkerName) != "", nil
}

func IsPoolSyncComplete(status string) bool {
	return strings.TrimSpace(status) == string(PoolSyncStatusPoolUpdatedSuccessfully)
}
