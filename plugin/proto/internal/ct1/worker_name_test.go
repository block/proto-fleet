package ct1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCT1WorkerNameDefaultsDoNotSplitMiningGroups(t *testing.T) {
	for _, mac := range []string{"02:00:00:00:00:01", "02:00:00:00:00:02"} {
		require.Empty(t, Capabilities().DefaultWorkerName(mac))
	}
}
