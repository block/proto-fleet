package poolconfigurations

import (
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	poolspb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/stretchr/testify/require"
)

func TestRenderWorkerPreservesPayoutAndExpandsOnlyExplicitSuffix(t *testing.T) {
	worker, err := RenderWorker("payout.existing.periods", "{miner_name}", "miner-1", "")
	require.NoError(t, err)
	require.Equal(t, "payout.existing.periods", worker, "no worker name must not append a dot")
	worker, err = RenderWorker("payout.existing.periods", "", "miner-1", "rack-2")
	require.NoError(t, err)
	require.Equal(t, "payout.existing.periods", worker)
	worker, err = RenderWorker("payout.existing.periods", "{miner_name}-{miner_id}", "miner-1", "rack-2")
	require.NoError(t, err)
	require.Equal(t, "payout.existing.periods.rack-2-miner-1", worker)
	_, err = RenderWorker(strings.Repeat("a", 255), "suffix", "", "")
	require.Error(t, err)
	_, err = RenderWorker("payout", "{miner_name}", "", "name with spaces")
	require.Error(t, err)
	_, err = RenderWorker("payout", "{unknown}", "", "")
	require.Error(t, err)
}

func TestPoolValidationBoundariesAndSupportedSchemes(t *testing.T) {
	for _, destination := range []string{
		"stratum+tcp://pool.example", "stratum+tcp://pool.example:65535", "stratum+ssl://127.0.0.1:443", "stratum+ws://[::1]:3333",
		"stratum2+tcp://pool.example:3333/79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798",
	} {
		options, err := Validate(RevisionOptions{Name: "  Choice  ", Slots: []SlotOptions{{URL: destination, Username: "payout.with.periods"}}})
		require.NoError(t, err, destination)
		require.NoError(t, protovalidate.Validate(&poolspb.PoolInput{Url: destination, Username: "validation"}), "saved hosted destinations must satisfy the canonical pool input contract")
		require.Equal(t, "Choice", options.Name)
	}
	for _, destination := range []string{"stratum+tcp://-invalid.example", "stratum+tcp://pool..example", "stratum+tcp://pool.example:", "stratum+tcp://pool.example?", "stratum+tcp://pool.example#", "stratum+tcp://[invalid-ip]", "stratum+tcp://", "stratum+tcp://localhost:3333", "stratum+tcp://10.0.0:3333", "stratum+tcp://pool.example:3333/", "stratum+tcp://pool.example:65536"} {
		require.False(t, ValidURL(destination), destination)
	}
	password := " secret with spaces "
	options := RevisionOptions{Name: strings.Repeat("n", 255), Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: strings.Repeat("u", 255), Password: &password}}}
	_, err := Validate(options)
	require.NoError(t, err)
	options.Slots[0].Username += "u"
	_, err = Validate(options)
	require.Error(t, err)
	options.Slots[0].Username = "payout"
	password = strings.Repeat("p", 256)
	_, err = Validate(options)
	require.Error(t, err)
	options.Slots = nil
	_, err = Validate(options)
	require.Error(t, err)
	options.Slots = make([]SlotOptions, 4)
	_, err = Validate(options)
	require.Error(t, err)
}
