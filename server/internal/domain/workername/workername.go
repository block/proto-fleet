package workername

import "strings"

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
