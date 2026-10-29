package control

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
)

func TestRegistryDiscoveryBudgetSharedAcrossCommandsAndNodes(t *testing.T) {
	ctx, cancel := discoverylimits.WithRequest(t.Context(), time.Minute)
	defer cancel()
	r := NewRegistry()
	nodes := []*Stream{r.Register(7), r.Register(8)}
	for _, node := range nodes {
		defer node.Unregister()
	}
	remaining := discoverylimits.MaxReportsPerRequest
	for i := 0; remaining > 0; i++ {
		nodeID := int64(7 + i%2)
		commandID := fmt.Sprintf("partition-%d", i)
		session, err := r.Send(ctx, nodeID, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &gatewaypb.ControlCommand{CommandId: commandID}, nil, ReportKindDiscovery, nil)
		require.NoError(t, err)
		<-nodes[i%2].Outgoing
		count := min(remaining, maxReportsPerCommand)
		// A wrong report kind may neither consume nor release the shared quota.
		require.ErrorIs(t, r.AdmitReport(nodeID, commandID, 1, ReportKindPair), errNoInFlightCommand)
		require.NoError(t, r.AdmitReport(nodeID, commandID, count, ReportKindDiscovery))
		remaining -= count
		if remaining == 0 {
			require.ErrorIs(t, r.AdmitReport(nodeID, commandID, 1, ReportKindDiscovery), ErrReportQuotaExceeded)
		}
		session.Close()
	}
	for i, node := range nodes {
		_, err := r.Send(ctx, int64(7+i), gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &gatewaypb.ControlCommand{CommandId: "after-budget"}, nil, ReportKindDiscovery, nil)
		require.ErrorIs(t, err, ErrReportQuotaExceeded)
		select {
		case <-node.Outgoing:
			t.Fatal("exhausted request dispatched another command")
		default:
		}
	}

	// Other requests and PAIR are independent of this discovery budget.
	other, cancelOther := discoverylimits.WithRequest(t.Context(), time.Minute)
	defer cancelOther()
	independent, err := r.Send(other, 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &gatewaypb.ControlCommand{CommandId: "independent"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer independent.Close()
	<-nodes[0].Outgoing
	require.NoError(t, r.AdmitReport(7, "independent", 1, ReportKindDiscovery))
	pair, err := r.Send(ctx, 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &gatewaypb.ControlCommand{CommandId: "pair"}, nil, ReportKindPair, nil)
	require.NoError(t, err)
	defer pair.Close()
	<-nodes[0].Outgoing
	require.NoError(t, r.AdmitReport(7, "pair", 1, ReportKindPair))
}

func TestRegistryDiscoveryBudgetRejectsWholeCrossingBatch(t *testing.T) {
	ctx, cancel := discoverylimits.WithRequest(t.Context(), time.Minute)
	defer cancel()
	budget := discoverylimits.BudgetFromContext(ctx)
	require.True(t, budget.Admit(discoverylimits.MaxReportsPerRequest-1))
	r := NewRegistry()
	stream := r.Register(7)
	defer stream.Unregister()
	session, err := r.Send(ctx, 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &gatewaypb.ControlCommand{CommandId: "crossing"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing
	require.ErrorIs(t, r.AdmitReport(7, "crossing", 2, ReportKindDiscovery), ErrReportQuotaExceeded)
	require.Zero(t, session.cmd.reported)
	require.True(t, budget.Exhausted())
	require.ErrorIs(t, r.AdmitReport(7, "crossing", 1, ReportKindDiscovery), ErrReportQuotaExceeded)
}
