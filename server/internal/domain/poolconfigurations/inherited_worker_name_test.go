package poolconfigurations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkerNameFromRendering(t *testing.T) {
	for _, tc := range []struct {
		name, username, template, rendered, want string
		ok                                       bool
	}{
		{"custom", "account", "{miner_name}", "account.rack-one", "rack-one", true},
		{"empty", "account", "{miner_name}", "account", "", true},
		{"payout periods", "account.region", "{miner_name}", "account.region.rack.one", "rack.one", true},
		{"literals and identifier", "account", "rack-{miner_id}-{miner_name}-end", "account.rack-controller-worker-end", "worker", true},
		{"repeated", "account", "{miner_name}-{miner_name}", "account.worker-worker", "worker", true},
		{"empty with literals", "account", "rack-{miner_name}", "account.rack-", "", true},
		{"mismatched repeated", "account", "{miner_name}-{miner_name}", "account.worker-otherx", "", false},
		{"wrong payout", "account", "{miner_name}", "other.worker", "", false},
		{"wrong literal", "account", "rack-{miner_name}-end", "account.rack-worker-bad", "", false},
		{"no placeholder", "account", "{miner_id}", "account.controller", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := workerNameFromRendering(tc.username, tc.template, "controller", tc.rendered)
			require.Equal(t, tc.ok, ok)
			if ok {
				require.Equal(t, tc.want, got)
			}
		})
	}
}
