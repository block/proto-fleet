package control

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
)

func acceptedBatch(count int) *pairingpb.DiscoverResponse {
	devices := make([]*pairingpb.Device, count)
	for i := range devices {
		devices[i] = &pairingpb.Device{}
	}
	return &pairingpb.DiscoverResponse{Devices: devices}
}

func TestRegistryDiscoveryBudgetSharedAcrossCommandsAndNodes(t *testing.T) {
	// Arrange
	ctx, cancel := discoverylimits.WithRequest(t.Context(), time.Minute)
	defer cancel()
	r := NewRegistry()
	nodes := []*Stream{r.Register(7), r.Register(8)}
	for _, node := range nodes {
		defer node.Unregister()
	}

	// Act
	remaining := discoverylimits.MaxReportsPerRequest
	for i := 0; remaining > 0; i++ {
		nodeID := int64(7 + i%2)
		commandID := fmt.Sprintf("partition-%d", i)
		session, err := r.Send(ctx, nodeID, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &gatewaypb.ControlCommand{CommandId: commandID}, nil, ReportKindDiscovery, nil)
		require.NoError(t, err)
		<-nodes[i%2].Outgoing
		count := min(remaining, maxReportsPerCommand)
		// A wrong report kind may neither admit nor consume the shared quota.
		require.ErrorIs(t, r.AdmitReport(nodeID, commandID, 1, ReportKindPair), errNoInFlightCommand)
		require.NoError(t, r.AdmitReport(nodeID, commandID, count, ReportKindDiscovery))
		r.PublishBatch(nodeID, commandID, acceptedBatch(count))
		remaining -= count
		session.Close()
	}

	// Assert
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

func TestRegistryDiscoveryBudgetChargesOnlyPublishedBatches(t *testing.T) {
	// Arrange
	ctx, cancel := discoverylimits.WithRequest(t.Context(), time.Minute)
	defer cancel()
	budget := discoverylimits.BudgetFromContext(ctx)
	r := NewRegistry()
	stream := r.Register(7)
	defer stream.Unregister()
	session, err := r.Send(WithCommandReportLimit(ctx, 1024), 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &gatewaypb.ControlCommand{CommandId: "scan"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing

	// Act: raw reports are all out of scope, so no batch is published
	require.NoError(t, r.AdmitReport(7, "scan", 1024, ReportKindDiscovery))

	// Assert
	require.False(t, budget.Exhausted())
	require.True(t, budget.Admit(discoverylimits.MaxReportsPerRequest), "unaccepted reports consumed shared quota")
}

func TestRegistryDiscoveryBudgetRejectsWholeCrossingBatch(t *testing.T) {
	// Arrange
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
	require.NoError(t, r.AdmitReport(7, "crossing", 2, ReportKindDiscovery))

	// Act
	r.PublishBatch(7, "crossing", acceptedBatch(2))

	// Assert
	select {
	case ev := <-session.Events():
		t.Fatalf("crossing batch was delivered: %v", ev)
	default:
	}
	require.True(t, budget.Exhausted())
	require.ErrorIs(t, r.AdmitReport(7, "crossing", 1, ReportKindDiscovery), ErrReportQuotaExceeded)
}

func TestSendCommandReportLimitLowersDiscoveryQuota(t *testing.T) {
	// Arrange
	r := NewRegistry()
	stream := r.Register(7)
	defer stream.Unregister()
	session, err := r.Send(WithCommandReportLimit(t.Context(), 3), 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &gatewaypb.ControlCommand{CommandId: "limited"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing

	// Act
	within := r.AdmitReport(7, "limited", 3, ReportKindDiscovery)
	beyond := r.AdmitReport(7, "limited", 1, ReportKindDiscovery)

	// Assert
	require.NoError(t, within)
	require.ErrorIs(t, beyond, ErrReportQuotaExceeded)
}
