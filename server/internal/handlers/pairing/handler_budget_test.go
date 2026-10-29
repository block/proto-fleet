package pairing

import (
	"context"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
)

type requestBudgetDiscoveryRunner struct {
	mu        sync.Mutex
	budgets   []*discoverylimits.ReportBudget
	deadlines []time.Time
	admitted  int
}

func (*requestBudgetDiscoveryRunner) DiscoveryNodeIDs(context.Context, int64) ([]int64, error) {
	return []int64{7, 8}, nil
}

func (r *requestBudgetDiscoveryRunner) RunOnNode(ctx context.Context, _ int64, _ string, _ *pb.DiscoverRequest, _ func(*pb.DiscoverResponse) error) error {
	budget := discoverylimits.BudgetFromContext(ctx)
	deadline, _ := ctx.Deadline()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.budgets = append(r.budgets, budget)
	r.deadlines = append(r.deadlines, deadline)
	if budget != nil && budget.Admit(discoverylimits.MaxReportsPerRequest/2+1) {
		r.admitted++
	}
	return nil
}

func TestDiscoverPublicFanoutSharesBudgetAndDeadlineAcrossNodes(t *testing.T) {
	// Arrange
	runner := &requestBudgetDiscoveryRunner{}
	client := discoveryClientForHandler(t, &Handler{discovery: runner})

	// Act
	stream, err := client.Discover(t.Context(), connect.NewRequest(&pb.DiscoverRequest{Mode: &pb.DiscoverRequest_NetworkScan{NetworkScan: &pb.NetworkScanModeRequest{UseFleetNodeLocalSubnet: true}}}))
	require.NoError(t, err)
	for stream.Receive() {
	}

	// Assert
	require.NoError(t, stream.Err())
	require.Len(t, runner.budgets, 2)
	require.NotNil(t, runner.budgets[0])
	require.Same(t, runner.budgets[0], runner.budgets[1])
	require.True(t, runner.budgets[0].Exhausted())
	require.Equal(t, 1, runner.admitted, "the second node must not receive a fresh quota")
	require.False(t, runner.deadlines[0].IsZero())
	require.Equal(t, runner.deadlines[0], runner.deadlines[1])
}
