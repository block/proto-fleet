// Package discovery dispatches server-initiated miner discovery to fleet nodes
// over the ControlStream and streams the results back. It owns the per-node
// run loop (validate -> send command -> drain batches until ack) shared by the
// operator-facing single-node RPC (handlers/fleetnode/admin) and the cloud
// "Find miners" fan-out (handlers/pairing), plus the helpers that decide which
// nodes a fan-out should target.
package discovery

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
	"github.com/block/proto-fleet/server/internal/domain/netutil"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
)

// DiscoverCommandTimeout bounds how long RunOnNode waits for the agent's batches
// and ack, so a silent node can't pin operator streams and registry slots. Must
// exceed the agent's scan budget (commandTimeout, 10m) plus report/ack slack: too
// short frees the slot mid-scan, the agent's ack is rejected as stale, and a new
// command dispatches while the node is still busy. Var for tests.
var DiscoverCommandTimeout = 12 * time.Minute

// nodeLister is the subset of enrollment.Service that fan-out targeting needs.
type nodeLister interface {
	ListFleetNodes(ctx context.Context, orgID int64) ([]enrollment.FleetNodeListing, error)
}

// nodeRegistry is the slice of control.Registry this service needs: enumerate
// connected nodes and dispatch a command to one. Narrowing it (like nodeLister)
// makes the coupling explicit and lets tests inject a fake without a Registry.
type nodeRegistry interface {
	ConnectedFleetNodeIDs() []int64
	CommandProtocolUpgradeRequired(fleetNodeID int64) bool
	Send(ctx context.Context, fleetNodeID int64, minimumCommandProtocolVersion gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand, scope control.ReportScope, kind control.ReportKind, pair *control.PairMeta) (*control.Session, error)
}

// Service runs discovery commands against connected fleet nodes.
type Service struct {
	registry   nodeRegistry
	enrollment nodeLister
}

func NewService(registry nodeRegistry, enrollmentSvc nodeLister) *Service {
	return &Service{registry: registry, enrollment: enrollmentSvc}
}

// EligibleNodeIDs returns the confirmed, connected nodes that support current
// discovery commands.
func (s *Service) EligibleNodeIDs(ctx context.Context, orgID int64) ([]int64, error) {
	nodes, err := s.enrollment.ListFleetNodes(ctx, orgID)
	if err != nil {
		return nil, err
	}
	confirmed := make(map[int64]struct{}, len(nodes))
	for _, n := range nodes {
		if n.EnrollmentStatus == enrollment.FleetNodeStatusConfirmed {
			confirmed[n.ID] = struct{}{}
		}
	}
	connected := s.registry.ConnectedFleetNodeIDs()
	out := make([]int64, 0, len(connected))
	for _, nodeID := range connected {
		if _, ok := confirmed[nodeID]; ok && !s.registry.CommandProtocolUpgradeRequired(nodeID) {
			out = append(out, nodeID)
		}
	}
	return out, nil
}

// RunOnNode validates req, builds the report scope, dispatches the command over
// the node's ControlStream, and invokes onBatch for each discovered-device batch
// until the node acks (or the command times out / the stream drops). It returns
// nil on an OK or PARTIAL ack, and an error otherwise, including any non-nil
// error returned by onBatch, which is treated as terminal (the caller's stream
// is gone, so there is nothing left to forward).
func (s *Service) RunOnNode(ctx context.Context, fleetNodeID int64, req *pairingpb.DiscoverRequest, onBatch func(*pairingpb.DiscoverResponse) error) error {
	req = requestForNode(req)
	if err := ValidateRequest(req); err != nil {
		return err
	}

	payload, err := proto.Marshal(&gatewaypb.AgentCommand{
		Command: &gatewaypb.AgentCommand_Discover{Discover: req},
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("marshal discover payload: %v", err)
	}

	cmd := &gatewaypb.ControlCommand{CommandId: id.GenerateID(), Payload: payload}
	return control.RunCommand(ctx, s.registry, fleetNodeID, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, cmd, buildReportScope(req), control.ReportKindDiscovery, nil, DiscoverCommandTimeout, "discovery",
		func(ev control.CommandEvent) (terminal bool, err error) {
			if ev.Batch != nil {
				if sendErr := onBatch(ev.Batch); sendErr != nil {
					return true, sendErr
				}
			}
			return false, nil
		})
}

// requestForNode translates the shared request flag into the sentinel understood
// by the Fleet Node command runner. False preserves the target.
func requestForNode(req *pairingpb.DiscoverRequest) *pairingpb.DiscoverRequest {
	if req == nil || req.GetNmap() == nil || !req.GetNmap().GetUseFleetNodeLocalSubnet() {
		return req
	}
	out := proto.CloneOf(req)
	out.GetNmap().Target = netscan.LocalSubnetTarget
	return out
}

// ValidateRequest checks Fleet Node target and port limits before discovery starts.
// Automatic local-subnet scans validate ports here and resolve targets on the node.
func ValidateRequest(in *pairingpb.DiscoverRequest) error {
	switch m := in.GetMode().(type) {
	case *pairingpb.DiscoverRequest_IpList:
		if m.IpList == nil || len(m.IpList.GetIpAddresses()) == 0 {
			return fleeterror.NewInvalidArgumentError("ip_list.ip_addresses must not be empty")
		}
		if err := checkScanLimits(m.IpList.GetIpAddresses(), m.IpList.GetPorts()); err != nil {
			return err
		}
		// Every entry must be a valid IP or hostname, and IP literals must be
		// private. A malformed token (e.g. "bad/entry") is unresolvable for the
		// agent yet trips the scope matcher's hostname fallback, widening the
		// command to port-only scope. A public literal scans fine but every report
		// is rejected by validateReport (private-only), surfacing as a late
		// REPORT_FAILED. Hostnames resolve agent-side to an IP the server can't
		// check here, so they pass through.
		for _, e := range m.IpList.GetIpAddresses() {
			target, err := netscan.ParseAddrTarget(e)
			if err != nil {
				return fleeterror.NewInvalidArgumentErrorf("ip_list entry %q is not a valid IP address or hostname", e)
			}
			if !target.IsPrivate() {
				return fleeterror.NewInvalidArgumentErrorf("ip_list entry %q is not a private (RFC1918/RFC4193) address", e)
			}
		}
		return nil
	case *pairingpb.DiscoverRequest_IpRange:
		if _, _, err := validatedIPv4Range(m.IpRange.GetStartIp(), m.IpRange.GetEndIp()); err != nil {
			return err
		}
		if err := checkScanLimits(nil, m.IpRange.GetPorts()); err != nil {
			return err
		}
		return nil
	case *pairingpb.DiscoverRequest_Nmap:
		target := m.Nmap.GetTarget()
		// The local-subnet flag or sentinel defers the target to the agent (it scans
		// its own private subnet(s)), so there is nothing to validate here; the
		// report scope (buildReportScope) and validateReport still confine reports
		// to private addresses.
		if m.Nmap.GetUseFleetNodeLocalSubnet() || target == netscan.LocalSubnetTarget {
			if err := checkScanLimits(nil, m.Nmap.GetPorts()); err != nil {
				return err
			}
			return nil
		}
		// Apply the same target grammar and CIDR breadth cap as the agent.
		parsed, err := netscan.ParseBoundedTarget(target)
		if err != nil {
			return fleeterror.NewInvalidArgumentError(err.Error())
		}
		// A public target scans fine but every report comes back non-private and
		// is rejected by validateReport, so fail fast. Hostnames resolve agent-side
		// and pass through (the report validator still guards what they return).
		if !parsed.IsPrivate() {
			return fleeterror.NewInvalidArgumentError("nmap target must be within a private (RFC1918/RFC4193) range")
		}
		if err := checkScanLimits(nil, m.Nmap.GetPorts()); err != nil {
			return err
		}
		return nil
	case *pairingpb.DiscoverRequest_Mdns:
		return fleeterror.NewInvalidArgumentError("mdns discovery is not supported on fleet nodes")
	default:
		return fleeterror.NewInvalidArgumentError("discover request mode is required")
	}
}

// checkScanLimits enforces the agent's per-command caps (via discoverylimits)
// and rejects malformed ports before dispatch, so an over-cap or invalid request
// fails fast with a validation error instead of a late agent BAD_REQUEST ack.
// The proto caps are the wire ceiling; these are the real limits.
func checkScanLimits(ipAddresses, ports []string) error {
	if len(ipAddresses) > discoverylimits.MaxScanTargets {
		return fleeterror.NewInvalidArgumentErrorf("too many targets: %d exceeds the limit of %d", len(ipAddresses), discoverylimits.MaxScanTargets)
	}
	if len(ports) > 0 {
		if _, err := netscan.Ports(ports, nil); err != nil {
			return fleeterror.NewInvalidArgumentError(err.Error())
		}
	}

	return nil
}

func validatedIPv4Range(startStr, endStr string) (uint32, uint32, error) {
	startAddr, err := netutil.ParseIPv4(startStr)
	if err != nil {
		return 0, 0, fleeterror.NewInvalidArgumentErrorf("invalid start_ip: %v", err)
	}
	endAddr, err := netutil.ParseIPv4(endStr)
	if err != nil {
		return 0, 0, fleeterror.NewInvalidArgumentErrorf("invalid end_ip: %v", err)
	}
	// Both ends must be private. The MaxScanTargets cap below keeps the range far
	// smaller than the gap between RFC1918 blocks, so private endpoints imply a
	// fully private range. A public range scans fine but every report is rejected
	// by validateReport, surfacing as a late REPORT_FAILED.
	if !startAddr.IsPrivate() || !endAddr.IsPrivate() {
		return 0, 0, fleeterror.NewInvalidArgumentError("ip range must be within a private (RFC1918) range")
	}
	start, end := netutil.IPv4ToUint32(startAddr), netutil.IPv4ToUint32(endAddr)
	if end < start {
		return 0, 0, fleeterror.NewInvalidArgumentError("end_ip must be >= start_ip")
	}
	// Skip the network (.0) and gateway (.1) start addresses, matching the agent
	// and server discovery; gateways answer on many ports and look like miners.
	start = netutil.AdjustIPv4RangeStart(start)
	if end < start {
		return 0, 0, fleeterror.NewInvalidArgumentError("ip range covers only network/gateway addresses")
	}
	// uint64 math so a range ending at 255.255.255.255 can't wrap (in uint32,
	// end-start+1 would overflow to 0, bypassing the cap and never terminating).
	size := uint64(end) - uint64(start) + 1
	if size > discoverylimits.MaxScanTargets {
		return 0, 0, fleeterror.NewInvalidArgumentErrorf("ip range exceeds %d addresses", discoverylimits.MaxScanTargets)
	}
	return start, end, nil
}
