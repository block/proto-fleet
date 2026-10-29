package gateway_test

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/discovery"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	pairinghandler "github.com/block/proto-fleet/server/internal/handlers/pairing"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type scaleNodes []enrollment.FleetNodeListing

func (n scaleNodes) ListFleetNodes(context.Context, int64) ([]enrollment.FleetNodeListing, error) {
	return n, nil
}

// This opt-in qualification uses real gateway persistence and PostgreSQL, with
// simulated node I/O. It makes no claim about physical driver or host capacity.
func TestFleetNodeScaleDiscoveryAndPairing(t *testing.T) {
	if os.Getenv("FLEETNODE_SCALE_TEST") != "1" {
		t.Skip("set FLEETNODE_SCALE_TEST=1 for the 50,000-miner qualification")
	}
	const total = 50000
	const nodeCount = 13
	const perNode = (total + nodeCount - 1) / nodeCount
	h := newControlHarness(t)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	pairingSvc := pairing.NewService(sqlstores.NewSQLFleetNodePairingStore(h.db), sqlstores.NewSQLFleetNodeEnrollmentStore(h.db), sqlstores.NewSQLTransactor(h.db)).WithProvisioning(sqlstores.NewSQLDeviceStore(h.db), sqlstores.NewSQLDiscoveredDeviceStore(h.db), h.registry)
	var nodes scaleNodes
	var discovered, paired atomic.Int64
	failures := make(chan error, 2*nodeCount)
	var workers sync.WaitGroup
	startIP := netip.MustParseAddr("10.30.0.1")
	nextIP := startIP
	for i := range nodeCount {
		first := nextIP
		count := min(perNode, total-i*perNode)
		var last netip.Addr
		for range count {
			last = nextIP
			nextIP = nextIP.Next()
		}
		id := h.fleetNodeID
		if i > 0 {
			key := make([]byte, 32)
			key[0] = byte(i)
			require.NoError(t, h.db.QueryRowContext(ctx, `INSERT INTO fleet_node (org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES (1,$1,$2,$2,'CONFIRMED') RETURNING id`, fmt.Sprintf("scale-%d", i), key).Scan(&id))
		}
		nodes = append(nodes, enrollment.FleetNodeListing{FleetNode: enrollment.FleetNode{ID: id, OrgID: 1, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}})
		stream, err := h.registry.RegisterAuthenticated(id, fmt.Sprintf("scale-session-%d", i), gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
		require.NoError(t, err)
		stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{first.String() + "-" + last.String()}})
		workers.Add(1)
		go func(nodeID int64, stream *control.Stream) {
			defer workers.Done()
			defer stream.Unregister()
			fail := func(err error) { failures <- err; cancel() }
			subjectCtx := authn.SetInfo(ctx, &auth.Subject{FleetNodeID: nodeID, OrgID: 1})
			for {
				select {
				case <-ctx.Done():
					return
				case <-stream.Done:
					return
				case command := <-stream.Outgoing:
					env := &gatewaypb.AgentCommand{}
					if err := proto.Unmarshal(command.Payload, env); err != nil {
						fail(err)
						return
					}
					if scan := env.GetDiscover(); scan != nil {
						target, err := netscan.ParseTarget(scan.GetNetworkScan().GetTarget())
						if err != nil {
							fail(err)
							return
						}
						var reports []*gatewaypb.DiscoveredDeviceReport
						for address := range target.Addresses() {
							reports = append(reports, &gatewaypb.DiscoveredDeviceReport{DeviceIdentifier: "scale:" + address.String(), IpAddress: address.String(), Port: "4028", UrlScheme: "http", DriverName: "virtual", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_RIG})
						}
						if len(reports) > 256 {
							fail(fmt.Errorf("scan has %d targets", len(reports)))
							return
						}
						response, err := h.handler.ReportDiscoveredDevices(subjectCtx, connect.NewRequest(&gatewaypb.ReportDiscoveredDevicesRequest{CommandId: command.CommandId, Devices: reports}))
						if err != nil {
							fail(err)
							return
						}
						if response.Msg.GetAcceptedCount() != int64(len(reports)) || response.Msg.GetRejectedCount() != 0 {
							fail(fmt.Errorf("discovery rejected targets: %v", response.Msg))
							return
						}
					} else if request := env.GetPair(); request != nil {
						if len(request.Targets) > 128 {
							fail(fmt.Errorf("pair command has %d targets", len(request.Targets)))
							return
						}
						results := make([]*gatewaypb.FleetNodePairResult, 0, len(request.Targets))
						for _, target := range request.Targets {
							results = append(results, &gatewaypb.FleetNodePairResult{DeviceIdentifier: target.DeviceIdentifier, Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED, DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_RIG})
						}
						response, err := h.handler.ReportPairedDevices(subjectCtx, connect.NewRequest(&gatewaypb.ReportPairedDevicesRequest{CommandId: command.CommandId, Results: results}))
						if err != nil {
							fail(err)
							return
						}
						if response.Msg.GetAcceptedCount() != int64(len(results)) || response.Msg.GetRejectedCount() != 0 {
							fail(fmt.Errorf("pairing rejected targets: %v", response.Msg))
							return
						}
						paired.Add(response.Msg.GetAcceptedCount())
					} else {
						fail(fmt.Errorf("unexpected command"))
						return
					}
					stream.PublishAck(&gatewaypb.ControlAck{CommandId: command.CommandId, Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
				}
			}
		}(id, stream)
	}
	defer func() { cancel(); workers.Wait() }()
	discoverySvc := discovery.NewService(h.registry, nodes)
	run := func(operation func(int64) error) {
		var tasks sync.WaitGroup
		for _, node := range nodes {
			tasks.Add(1)
			go func(id int64) {
				defer tasks.Done()
				if err := operation(id); err != nil {
					failures <- err
					cancel()
				}
			}(node.ID)
		}
		tasks.Wait()
		select {
		case err := <-failures:
			require.NoError(t, err)
		default:
		}
		require.NoError(t, ctx.Err())
	}
	started := time.Now()
	run(func(id int64) error {
		return discoverySvc.RunOnNode(ctx, id, "scale node", &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_NetworkScan{NetworkScan: &pairingpb.NetworkScanModeRequest{UseFleetNodeLocalSubnet: true}}}, func(batch *pairingpb.DiscoverResponse) error {
			if batch.GetWarning() != "" {
				return fmt.Errorf("discovery warning: %s", batch.Warning)
			}
			discovered.Add(int64(len(batch.GetDevices())))
			return nil
		})
	})
	discoveryDuration := time.Since(started)
	require.Equal(t, int64(total), discovered.Load())
	var unique int
	require.NoError(t, h.db.QueryRowContext(ctx, `SELECT count(DISTINCT device_identifier) FROM discovered_device WHERE org_id=1 AND deleted_at IS NULL`).Scan(&unique))
	require.Equal(t, total, unique)
	rows, err := h.db.QueryContext(ctx, `SELECT device_identifier FROM discovered_device WHERE org_id=1 AND deleted_at IS NULL ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	var selected []string
	for rows.Next() {
		var identifier string
		require.NoError(t, rows.Scan(&identifier))
		selected = append(selected, identifier)
	}
	require.NoError(t, rows.Err())
	started = time.Now()
	operatorCtx := authn.SetInfo(ctx, &session.Info{AuthMethod: session.AuthMethodSession, UserID: 1, OrganizationID: 1})
	operatorCtx = middleware.WithEffectivePermissions(operatorCtx, authz.NewEffectivePermissions([]authz.Assignment{{AssignmentID: 1, ScopeType: authz.ScopeOrg, Permissions: []string{authz.PermMinerPair}}}))
	response, err := pairinghandler.NewHandler(nil, discoverySvc, pairingSvc).Pair(operatorCtx, connect.NewRequest(&pairingpb.PairRequest{
		DeviceSelector: &minercommandpb.DeviceSelector{SelectionType: &minercommandpb.DeviceSelector_IncludeDevices{IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: selected}}},
	}))
	require.NoError(t, err)
	require.Empty(t, response.Msg.GetFailedDeviceIds())
	pairingDuration := time.Since(started)
	require.Equal(t, int64(total), paired.Load())
	var bindings, owners, maxAssigned int
	require.NoError(t, h.db.QueryRowContext(ctx, `SELECT count(*),count(DISTINCT device_id) FROM fleet_node_device WHERE org_id=1`).Scan(&bindings, &owners))
	require.Equal(t, total, bindings)
	require.Equal(t, total, owners)
	require.NoError(t, h.db.QueryRowContext(ctx, `SELECT max(assigned) FROM (SELECT count(*) AS assigned FROM fleet_node_device WHERE org_id=1 GROUP BY fleet_node_id) counts`).Scan(&maxAssigned))
	require.LessOrEqual(t, maxAssigned, 4000)
	t.Logf("50,000 miners / 13 nodes: discovery=%s pairing=%s DB connection limit=%d", discoveryDuration, pairingDuration, h.db.Stats().MaxOpenConnections)
}
