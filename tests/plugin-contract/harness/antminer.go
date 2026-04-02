package harness

import (
	"fmt"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// StartAntminer starts the Antminer plugin binary with the given web port override
// and cache TTL disabled for testing.
func StartAntminer(t testing.TB, webPort int) sdk.Driver {
	t.Helper()

	env := map[string]string{
		"ANTMINER_WEB_PORT":        fmt.Sprintf("%d", webPort),
		"ANTMINER_STATUS_CACHE_TTL": "0s",
	}

	return StartPlugin(t, "antminer-plugin", "", env)
}
