package chat

import (
	"context"
	"encoding/json"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	poolsv1 "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
)

type staticPoolsHandler struct {
	pools []*poolsv1.Pool
}

func (h staticPoolsHandler) ListPools(context.Context, *connect.Request[poolsv1.ListPoolsRequest]) (*connect.Response[poolsv1.ListPoolsResponse], error) {
	return connect.NewResponse(&poolsv1.ListPoolsResponse{Pools: h.pools}), nil
}

func TestListPoolsOnlyDisclosesNamesToModelProvider(t *testing.T) {
	tools := NewFleetTools(nil, nil, staticPoolsHandler{pools: []*poolsv1.Pool{{
		PoolId:   42,
		PoolName: "Primary pool",
		Url:      "stratum+tcp://pool.example.com:3333",
		Username: "bc1q-wallet.worker-01",
	}}})

	output, err := tools.Execute(t.Context(), "list_pools", json.RawMessage(`{}`))

	require.NoError(t, err)
	assert.JSONEq(t, `{"pools":[{"name":"Primary pool"}]}`, output.Content)
	assert.NotContains(t, output.Content, "pool.example.com")
	assert.NotContains(t, output.Content, "bc1q-wallet")
	assert.NotContains(t, output.Content, "42")
}
