package pairing

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"

	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/discovery"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/nmaptarget"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
)

// Handler handles the Connect-RPC endpoints
type Handler struct {
	pairingSvc *pairing.Service
	// discovery fans supported requests out to eligible fleet nodes; nil keeps
	// discovery server-only.
	discovery fleetNodeDiscoveryRunner
	// fleetNodePairing lets the existing PairingService API route selected
	// fleet-node-discovered devices through ControlStream while keeping clients
	// agnostic to the pairing mechanism.
	fleetNodePairing *fleetnodepairing.Service
}

type fleetNodeDiscoveryRunner interface {
	EligibleNodeIDs(ctx context.Context, orgID int64) ([]int64, error)
	RunOnNode(ctx context.Context, fleetNodeID int64, req *pb.DiscoverRequest, onBatch func(*pb.DiscoverResponse) error) error
}

var _ pairingv1connect.PairingServiceHandler = &Handler{}

type fleetNodePairRoute struct {
	allDevices       bool
	routedExplicit   map[string]struct{}
	routedAllDevices map[string]struct{}
	remoteSucceeded  bool
	remoteErr        error
}

// NewHandler creates a new instance of Handler
func NewHandler(pairingSvc *pairing.Service, discoverySvc *discovery.Service, fleetNodePairingSvc *fleetnodepairing.Service) *Handler {
	return &Handler{
		pairingSvc:       pairingSvc,
		discovery:        discoverySvc,
		fleetNodePairing: fleetNodePairingSvc,
	}
}

// Discover implements pairingv1connect.PairingServiceHandler. Supported requests
// fan out to the server and every eligible fleet node, and merge into one stream.
func (h *Handler) Discover(ctx context.Context, r *connect.Request[pb.DiscoverRequest], s *connect.ServerStream[pb.DiscoverResponse]) error {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerPair, authz.ResourceContext{})
	if err != nil {
		return err
	}
	if h.discovery != nil {
		if err := validateManualNmapTarget(r.Msg); err != nil {
			return err
		}
	}
	slog.Debug("Discover: handling discover request", "payload", r.Msg)

	// A send failure (operator disconnected) cancels every source.
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Serialize the concurrent sources (cloud scan + each node) onto the one
	// stream and dedupe devices across them; a Send failure cancels the rest.
	fwd := newDedupForwarder(s.Send, cancel)

	var resultChan <-chan *pb.DiscoverResponse
	switch r.Msg.Mode.(type) {
	case *pb.DiscoverRequest_IpList:
		resultChan, err = h.pairingSvc.DiscoverWithIPList(streamCtx, r.Msg.GetIpList())
	case *pb.DiscoverRequest_IpRange:
		resultChan, err = h.pairingSvc.DiscoverWithIPRange(streamCtx, r.Msg.GetIpRange())
	case *pb.DiscoverRequest_Nmap:
		resultChan, err = h.pairingSvc.DiscoverWithNmap(streamCtx, r.Msg.GetNmap())
	case *pb.DiscoverRequest_Mdns:
		resultChan, err = h.pairingSvc.DiscoverWithMDNS(streamCtx, r.Msg.GetMdns())
	default:
		return fleeterror.NewInternalError("unsupported mode")
	}
	if err != nil {
		return err
	}

	nodeReq := fleetNodeDiscoveryRequest(r.Msg)
	h.forwardDiscoverySources(streamCtx, info.OrganizationID, resultChan, nodeReq, fwd)
	if err := fwd.failure(); err != nil {
		return err
	}
	// A client cancel/deadline drains the sources without a Send error; report it
	// rather than success. (A fan-out-budget expiry is not a client error.)
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return connect.NewError(connect.CodeDeadlineExceeded, ctxErr)
		}
		return fleeterror.NewCanceledError()
	}
	return nil
}

func (h *Handler) forwardDiscoverySources(
	ctx context.Context,
	organizationID int64,
	serverResults <-chan *pb.DiscoverResponse,
	nodeReq *pb.DiscoverRequest,
	fwd *dedupForwarder,
) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case result, ok := <-serverResults:
				if !ok {
					return
				}
				if err := fwd.forward(result); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if nodeReq != nil && h.discovery != nil {
		nodeIDs, err := h.discovery.EligibleNodeIDs(ctx, organizationID)
		if err != nil {
			// Fan-out is best-effort; a lookup failure must never break the
			// server scan. With zero connected nodes this is the same path.
			slog.Warn("skipping fleet node discovery fan-out", "error", err)
		} else {
			for _, nodeID := range nodeIDs {
				if ctx.Err() != nil {
					break
				}
				wg.Add(1)
				go func(nodeID int64) {
					defer wg.Done()
					if ctx.Err() != nil {
						return
					}
					// Each node is bounded by RunOnNode's per-node timeout.
					runErr := h.discovery.RunOnNode(ctx, nodeID, nodeReq, fwd.forward)
					// One node failing must not fail the scan, and is expected on
					// operator disconnect — stay quiet once ctx is done.
					if runErr != nil && ctx.Err() == nil {
						slog.Warn("fleet node discovery failed during server fan-out",
							"fleet_node_id", nodeID, "error", runErr)
					}
				}(nodeID)
			}
		}
	}

	wg.Wait()
}

// fleetNodeDiscoveryRequest returns requests supported by Fleet Nodes. The
// discovery service translates automatic local-subnet scans before dispatch.
func fleetNodeDiscoveryRequest(req *pb.DiscoverRequest) *pb.DiscoverRequest {
	switch req.GetMode().(type) {
	case *pb.DiscoverRequest_IpList, *pb.DiscoverRequest_IpRange, *pb.DiscoverRequest_Nmap:
		return req
	default:
		return nil
	}
}

// validateManualNmapTarget preflights explicit node-bound Nmap targets before
// local work begins.
func validateManualNmapTarget(req *pb.DiscoverRequest) error {
	if req.GetNmap() == nil || req.GetNmap().GetUseFleetNodeLocalSubnet() {
		return nil
	}
	if err := nmaptarget.Validate(req.GetNmap().GetTarget()); err != nil {
		return fleeterror.NewInvalidArgumentError(err.Error())
	}
	return nil
}

// Pair implements pairingv1connect.PairingServiceHandler.
func (h *Handler) Pair(ctx context.Context, r *connect.Request[pb.PairRequest]) (*connect.Response[pb.PairResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerPair, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}

	if resp, route, err := h.pairFleetNodeDevices(ctx, info.OrganizationID, info.UserID, r.Msg); err != nil {
		return nil, err
	} else if route.allDevices {
		var (
			cloudResp *pb.PairResponse
			err       error
		)
		if route.remoteSucceeded {
			cloudResp, err = h.pairingSvc.PairDevicesAllowAllFailed(ctx, r.Msg)
		} else {
			cloudResp, err = h.pairingSvc.PairDevices(ctx, r.Msg)
		}
		if err != nil {
			return handleAllDevicesCloudPairError(resp, route, err)
		}
		resp.FailedDeviceIds = mergeAllDevicesPairFailures(resp.GetFailedDeviceIds(), cloudResp.GetFailedDeviceIds(), route)
		return connect.NewResponse(resp), nil
	} else if len(route.routedExplicit) > 0 {
		remaining := remainingSelectedDeviceIdentifiers(r.Msg.GetDeviceSelector(), route.routedExplicit)
		if len(remaining) == 0 {
			if !route.remoteSucceeded && route.remoteErr != nil {
				return nil, route.remoteErr
			}
			if routedTargetsFailed(resp, route, route.routedExplicit) {
				return nil, fleeterror.NewInternalError("Failed to pair any devices")
			}
			return connect.NewResponse(resp), nil
		}
		cloudReq := &pb.PairRequest{
			Credentials:    r.Msg.GetCredentials(),
			DeviceSelector: includeDevicesSelector(remaining),
		}
		var (
			cloudResp *pb.PairResponse
			err       error
		)
		if route.remoteSucceeded {
			cloudResp, err = h.pairingSvc.PairDevicesAllowAllFailed(ctx, cloudReq)
		} else {
			cloudResp, err = h.pairingSvc.PairDevices(ctx, cloudReq)
		}
		if err != nil {
			return handleExplicitCloudPairError(route, err)
		}
		resp.FailedDeviceIds = sortedKeys(setFromSlice(append(resp.FailedDeviceIds, cloudResp.GetFailedDeviceIds()...)))
		return connect.NewResponse(resp), nil
	}

	resp, err := h.pairingSvc.PairDevices(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func handleExplicitCloudPairError(route fleetNodePairRoute, err error) (*connect.Response[pb.PairResponse], error) {
	if !route.remoteSucceeded && route.remoteErr != nil {
		return nil, route.remoteErr
	}
	return nil, err
}

func (h *Handler) pairFleetNodeDevices(ctx context.Context, orgID, userID int64, req *pb.PairRequest) (*pb.PairResponse, fleetNodePairRoute, error) {
	route := fleetNodePairRoute{
		routedExplicit:   map[string]struct{}{},
		routedAllDevices: map[string]struct{}{},
	}
	resp := &pb.PairResponse{}
	if h.discovery == nil || h.fleetNodePairing == nil {
		return resp, route, nil
	}

	selected := selectedDeviceIdentifiers(req.GetDeviceSelector())
	allDevicesFilter := allDevicesFilter(req.GetDeviceSelector())
	if len(selected) == 0 && allDevicesFilter == nil {
		return resp, route, nil
	}

	nodeIDs, err := h.discovery.EligibleNodeIDs(ctx, orgID)
	if err != nil {
		slog.Warn("skipping fleet node pairing route", "error", err)
		return resp, route, nil
	}
	if len(nodeIDs) == 0 {
		return resp, route, nil
	}

	pending := setFromSlice(selected)
	sortedSelected := sortedKeys(pending)
	failed := map[string]struct{}{}
	for _, nodeID := range nodeIDs {
		if allDevicesFilter != nil {
			if err := h.pairAllMatchingFleetNodeDevices(ctx, nodeID, orgID, userID, allDevicesFilter, req, &route, failed); err != nil {
				return nil, fleetNodePairRoute{}, err
			}
			continue
		}

		targetIDs := pendingSortedIdentifiers(sortedSelected, pending)
		for start := 0; start < len(targetIDs); start += fleetnodepairing.MaxPairBatch {
			end := start + fleetnodepairing.MaxPairBatch
			if end > len(targetIDs) {
				end = len(targetIDs)
			}
			targets, err := h.fleetNodePairing.ResolvePairTargets(ctx, nodeID, orgID, targetIDs[start:end], false, req.GetCredentials())
			if err != nil {
				return nil, fleetNodePairRoute{}, err
			}
			if len(targets) == 0 {
				continue
			}
			recordFleetNodePairTargets(targets, false, route.routedExplicit, route.routedAllDevices, failed, pending)
			if err := h.pairFleetNodeTargetBatch(ctx, nodeID, orgID, userID, req, targets, false, &route, failed); err != nil {
				slog.Warn("fleet node pairing command failed; returning failed targets in pair response",
					"fleet_node_id", nodeID, "error", err)
				if route.remoteErr == nil {
					route.remoteErr = err
				}
				break
			}
		}
		if len(pending) == 0 {
			break
		}
	}

	resp.FailedDeviceIds = sortedKeys(failed)
	return resp, route, nil
}

func (h *Handler) pairAllMatchingFleetNodeDevices(ctx context.Context, nodeID, orgID, userID int64, allDevicesFilter *minercommandv1.DeviceFilter, req *pb.PairRequest, route *fleetNodePairRoute, failed map[string]struct{}) error {
	var cursorID *int64
	for {
		targets, nextCursor, err := h.fleetNodePairing.ResolvePairTargetsByFilterPage(ctx, nodeID, orgID, allDevicesFilter, req.GetCredentials(), cursorID)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return nil
		}
		recordFleetNodePairTargets(targets, true, route.routedExplicit, route.routedAllDevices, failed, nil)
		if err := h.pairFleetNodeTargetBatch(ctx, nodeID, orgID, userID, req, targets, true, route, failed); err != nil {
			slog.Warn("fleet node pairing command failed; returning failed targets in pair response",
				"fleet_node_id", nodeID, "error", err)
			if route.remoteErr == nil {
				route.remoteErr = err
			}
			return nil
		}
		cursorID = nextCursor
		if cursorID == nil {
			return nil
		}
	}
}

func recordFleetNodePairTargets(targets []*pb.FleetNodePairTarget, allDevices bool, routedExplicit, routedAllDevices, failed, pending map[string]struct{}) {
	for _, target := range targets {
		id := target.GetDeviceIdentifier()
		if allDevices {
			routedAllDevices[id] = struct{}{}
		} else {
			routedExplicit[id] = struct{}{}
		}
		failed[id] = struct{}{}
		if pending != nil {
			delete(pending, id)
		}
	}
}

func (h *Handler) pairFleetNodeTargetBatch(ctx context.Context, nodeID, orgID, userID int64, req *pb.PairRequest, targets []*pb.FleetNodePairTarget, allDevices bool, route *fleetNodePairRoute, failed map[string]struct{}) error {
	route.allDevices = route.allDevices || allDevices
	assignedBy := userID
	return h.fleetNodePairing.PairOnNode(ctx, nodeID, targets, req.GetCredentials(), orgID, &assignedBy, func(results []*gatewaypb.FleetNodePairResult) error {
		for _, result := range results {
			id := result.GetDeviceIdentifier()
			if _, ok := failed[id]; !ok {
				continue
			}
			if result.GetOutcome() == gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED {
				delete(failed, id)
				route.remoteSucceeded = true
			} else {
				failed[id] = struct{}{}
			}
		}
		return nil
	})
}

func mergeAllDevicesPairFailures(remoteFailedIDs, cloudFailedIDs []string, route fleetNodePairRoute) []string {
	failed := setFromSlice(remoteFailedIDs)
	for _, id := range cloudFailedIDs {
		if _, routed := route.routedAllDevices[id]; routed {
			if _, remoteFailed := failed[id]; !remoteFailed {
				continue
			}
		}
		failed[id] = struct{}{}
	}
	return sortedKeys(failed)
}

func routedTargetsFailed(resp *pb.PairResponse, route fleetNodePairRoute, routed map[string]struct{}) bool {
	return !route.remoteSucceeded && route.remoteErr == nil && len(routed) > 0 && len(resp.GetFailedDeviceIds()) > 0
}

func handleAllDevicesCloudPairError(resp *pb.PairResponse, route fleetNodePairRoute, err error) (*connect.Response[pb.PairResponse], error) {
	if isNoCloudPairTargetsError(err) {
		if route.remoteSucceeded {
			slog.Warn("cloud pairing matched no devices after fleet node pairing succeeded; returning partial fleet node result",
				"error", err)
			return connect.NewResponse(resp), nil
		}
		if routedTargetsFailed(resp, route, route.routedAllDevices) {
			slog.Warn("cloud pairing matched no devices after all fleet node pairing attempts failed",
				"error", err)
			return nil, fleeterror.NewInternalError("Failed to pair any devices")
		}
	}
	if route.remoteSucceeded {
		slog.Warn("cloud pairing failed after fleet node pairing succeeded",
			"error", err)
		return nil, err
	}
	if route.remoteErr != nil {
		return nil, route.remoteErr
	}
	return nil, err
}

func isNoCloudPairTargetsError(err error) bool {
	var fleetErr fleeterror.FleetError
	return errors.As(err, &fleetErr) &&
		fleetErr.GRPCCode == connect.CodeInvalidArgument &&
		fleetErr.DebugMessage == "no devices match the selector"
}

func selectedDeviceIdentifiers(selector *minercommandv1.DeviceSelector) []string {
	selection := selector.GetIncludeDevices()
	if selection == nil {
		return nil
	}
	return selection.GetDeviceIdentifiers()
}

func allDevicesFilter(selector *minercommandv1.DeviceSelector) *minercommandv1.DeviceFilter {
	if selector == nil {
		return nil
	}
	allDevices := selector.GetAllDevices()
	if allDevices == nil {
		return nil
	}
	return allDevices
}

func remainingSelectedDeviceIdentifiers(selector *minercommandv1.DeviceSelector, routed map[string]struct{}) []string {
	selected := selectedDeviceIdentifiers(selector)
	if len(selected) == 0 {
		return nil
	}
	remaining := make([]string, 0, len(selected))
	for _, id := range selected {
		if _, ok := routed[id]; !ok {
			remaining = append(remaining, id)
		}
	}
	return remaining
}

func pendingSortedIdentifiers(sortedSelected []string, pending map[string]struct{}) []string {
	if len(pending) == 0 {
		return nil
	}
	out := make([]string, 0, len(pending))
	for _, id := range sortedSelected {
		if _, ok := pending[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

func includeDevicesSelector(deviceIdentifiers []string) *minercommandv1.DeviceSelector {
	return &minercommandv1.DeviceSelector{
		SelectionType: &minercommandv1.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonv1.DeviceIdentifierList{DeviceIdentifiers: deviceIdentifiers},
		},
	}
}

func setFromSlice(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}
