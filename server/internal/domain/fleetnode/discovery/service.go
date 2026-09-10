// Package discovery dispatches server-initiated miner discovery to fleet nodes
// over the ControlStream and streams the results back. It owns the per-node
// run loop (validate -> send command -> drain batches until ack) shared by the
// operator-facing single-node RPC (handlers/fleetnode/admin) and the cloud
// "Find miners" fan-out (handlers/pairing), plus the helpers that decide which
// nodes a fan-out should target.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
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
// until the node acks (or the command times out / the stream drops). It emits
// runtime failures as sourced warnings while retaining earlier batches. Validation,
// authentication, and onBatch failures remain errors. Caller cancellation is quiet.
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
	var callbackErr error
	forward := func(batch *pairingpb.DiscoverResponse) error {
		callbackErr = onBatch(batch)
		return callbackErr
	}
	warning := func(detail string) error {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		return forward(&pairingpb.DiscoverResponse{Warning: fmt.Sprintf("Fleet Node %d: %s", fleetNodeID, detail)})
	}
	err = control.RunCommand(ctx, s.registry, fleetNodeID, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, cmd, buildReportScope(req), control.ReportKindDiscovery, nil, DiscoverCommandTimeout, "discovery",
		func(ev control.CommandEvent) (terminal bool, err error) {
			if ev.Batch != nil {
				if sendErr := forward(ev.Batch); sendErr != nil {
					return true, sendErr
				}
			}
			if ev.Ack.GetCode() == gatewaypb.AckCode_ACK_CODE_PARTIAL {
				detail := ev.Ack.GetErrorMessage()
				if detail == "" {
					detail = "discovery completed partially"
				}
				return true, warning(detail)
			}
			return false, nil
		})
	if callbackErr != nil {
		return callbackErr
	}
	if err == nil || errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	if fleeterror.IsInvalidArgumentError(err) || fleeterror.IsAuthenticationError(err) || fleeterror.IsForbiddenError(err) {
		return err
	}
	detail := err.Error()
	var fleetErr fleeterror.FleetError
	var connectErr *connect.Error
	if errors.As(err, &fleetErr) {
		detail = fleetErr.DebugMessage
	} else if errors.As(err, &connectErr) {
		detail = connectErr.Message()
	}
	return warning(detail)
}

// requestForNode translates the shared request flag into the sentinel understood
// by the Fleet Node command runner. False preserves the target.
func requestForNode(req *pairingpb.DiscoverRequest) *pairingpb.DiscoverRequest {
	if req == nil || req.GetNetworkScan() == nil || !req.GetNetworkScan().GetUseFleetNodeLocalSubnet() {
		return req
	}
	out := proto.CloneOf(req)
	out.GetNetworkScan().Target = netscan.LocalSubnetTarget
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
		if _, err := validatedIPv4Range(m.IpRange.GetStartIp(), m.IpRange.GetEndIp()); err != nil {
			return err
		}
		if err := checkScanLimits(nil, m.IpRange.GetPorts()); err != nil {
			return err
		}
		return nil
	case *pairingpb.DiscoverRequest_NetworkScan:
		target := m.NetworkScan.GetTarget()
		// The local-subnet flag or sentinel defers the target to the agent (it scans
		// its own private subnet(s)), so there is nothing to validate here; the
		// report scope (buildReportScope) and validateReport still confine reports
		// to private addresses.
		if m.NetworkScan.GetUseFleetNodeLocalSubnet() || target == netscan.LocalSubnetTarget {
			if err := checkScanLimits(nil, m.NetworkScan.GetPorts()); err != nil {
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
			return fleeterror.NewInvalidArgumentError("network scan target must be within a private (RFC1918/RFC4193) range")
		}
		if err := checkScanLimits(nil, m.NetworkScan.GetPorts()); err != nil {
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
// These checks match the proto caps and also protect internal dispatch callers.
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

func validatedIPv4Range(startStr, endStr string) (netscan.Target, error) {
	target, err := netscan.Range(startStr, endStr)
	if err != nil {
		return netscan.Target{}, fleeterror.NewInvalidArgumentError(err.Error())
	}
	if !target.IsPrivate() {
		return netscan.Target{}, fleeterror.NewInvalidArgumentError("ip range must be within a private (RFC1918) range")
	}
	if target.Count() > discoverylimits.MaxScanTargets {
		return netscan.Target{}, fleeterror.NewInvalidArgumentErrorf("ip range exceeds %d addresses", discoverylimits.MaxScanTargets)
	}
	return target, nil
}
