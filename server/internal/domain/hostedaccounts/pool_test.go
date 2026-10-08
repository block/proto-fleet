package hostedaccounts

import (
	"errors"
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/stretchr/testify/require"
)

func TestPoolStorageErrorsRedactValuesAndPreserveTransactionRetry(t *testing.T) {
	cause := &pgconn.PgError{Code: "40001", Message: "sensitive pool JSON"}
	err := poolStorageError(cause)
	require.NotContains(t, err.Error(), "sensitive pool JSON")
	require.True(t, db.IsRetryablePostgresError(err))
	var recovered *pgconn.PgError
	require.True(t, errors.As(err, &recovered))
	require.Same(t, cause, recovered)
}

func TestRenderPoolWorkerPreservesPayoutAndExpandsOnlyExplicitSuffix(t *testing.T) {
	worker, err := RenderPoolWorker("payout.existing.periods", "", "miner-1", "rack-2")
	require.NoError(t, err)
	require.Equal(t, "payout.existing.periods", worker)
	worker, err = RenderPoolWorker("payout.existing.periods", "{miner_name}-{miner_id}", "miner-1", "rack-2")
	require.NoError(t, err)
	require.Equal(t, "payout.existing.periods.rack-2-miner-1", worker)
	_, err = RenderPoolWorker(strings.Repeat("a", 255), "suffix", "", "")
	require.Error(t, err)
	_, err = RenderPoolWorker("payout", "{miner_name}", "", "name with spaces")
	require.Error(t, err)
	_, err = RenderPoolWorker("payout", "{unknown}", "", "")
	require.Error(t, err)
}

func TestPoolValidationBoundariesAndSupportedSchemes(t *testing.T) {
	for _, destination := range []string{
		"stratum+tcp://pool.example", "stratum+tcp://pool.example:65535", "stratum+ssl://127.0.0.1:443", "stratum+ws://[::1]:3333",
		"stratum2+tcp://pool.example:3333/79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798",
	} {
		options, err := validatePoolOptions(PoolRevisionOptions{Name: "  Choice  ", Slots: []PoolSlotOptions{{URL: destination, Username: "payout.with.periods"}}})
		require.NoError(t, err, destination)
		require.NoError(t, protovalidate.Validate(&poolspb.PoolConfig{Url: destination}), "saved hosted destinations must satisfy the host pool contract")
		require.Equal(t, "Choice", options.Name)
	}
	for _, destination := range []string{"stratum+tcp://-invalid.example", "stratum+tcp://pool..example", "stratum+tcp://pool.example:", "stratum+tcp://pool.example?", "stratum+tcp://pool.example#", "stratum+tcp://[invalid-ip]", "stratum+tcp://", "stratum+tcp://localhost:3333", "stratum+tcp://10.0.0:3333", "stratum+tcp://pool.example:3333/", "stratum+tcp://pool.example:65536"} {
		require.False(t, validPoolURL(destination), destination)
	}
	password := " secret with spaces "
	options := PoolRevisionOptions{Name: strings.Repeat("n", 255), Slots: []PoolSlotOptions{{URL: "stratum+tcp://pool.example", Username: strings.Repeat("u", 255), Password: &password}}}
	_, err := validatePoolOptions(options)
	require.NoError(t, err)
	options.Slots[0].Username += "u"
	_, err = validatePoolOptions(options)
	require.Error(t, err)
	options.Slots[0].Username = "payout"
	password = strings.Repeat("p", 256)
	_, err = validatePoolOptions(options)
	require.Error(t, err)
	options.Slots = nil
	_, err = validatePoolOptions(options)
	require.Error(t, err)
	options.Slots = make([]PoolSlotOptions, 4)
	_, err = validatePoolOptions(options)
	require.Error(t, err)
}
