package pairing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/discovery"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
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
	DiscoveryNodeIDs(ctx context.Context, orgID int64) ([]int64, error)
	RunOnNode(ctx context.Context, fleetNodeID int64, source string, req *pb.DiscoverRequest, onBatch func(*pb.DiscoverResponse) error) error
}

var _ pairingv1connect.PairingServiceHandler = &Handler{}

const (
	pairLookupBatchSize          = 1024
	discoveryLookupBatchSize     = 1024
	discoveryLookupBatchMaxDelay = 100 * time.Millisecond
)

// pairFleetNodeDeadline bounds the fleet node fan-out of one Pair call so the
// response, with its failed device IDs, arrives before the reverse proxy's
// 75 minute read timeout. A command already accepted by a node still finishes
// (up to PairCommandTimeout) after the deadline. Var for tests.
var pairFleetNodeDeadline = 45 * time.Minute

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
	nodeReq := fleetNodeDiscoveryRequest(r.Msg)
	slog.Debug("Discover: handling discover request", "payload", r.Msg)

	streamCtx, cancel := discovery.NewRequestContext(ctx)
	defer cancel()

	// Serialize the concurrent sources (cloud scan + each node) onto the one
	// stream and dedupe devices across them; a Send failure cancels the rest.
	fwd := newDedupForwarder(
		func(ctx context.Context, devices []*pb.Device) error {
			if err := h.pairingSvc.AnnotateAlreadyPaired(ctx, info.OrganizationID, devices); err != nil {
				return fleeterror.NewInternalErrorf("determine discovered device pairing state: %v", err)
			}
			return nil
		},
		s.Send,
		cancel,
	)

	var resultChan <-chan *pb.DiscoverResponse
	switch r.Msg.Mode.(type) {
	case *pb.DiscoverRequest_IpList:
		resultChan, err = h.pairingSvc.DiscoverWithIPList(streamCtx, r.Msg.GetIpList())
	case *pb.DiscoverRequest_IpRange:
		resultChan, err = h.pairingSvc.DiscoverWithIPRange(streamCtx, r.Msg.GetIpRange())
	case *pb.DiscoverRequest_NetworkScan:
		if h.discovery != nil && discovery.ScansLocalSubnet(r.Msg.GetNetworkScan()) {
			err = discovery.ValidateRequest(r.Msg)
		} else {
			resultChan, err = h.pairingSvc.DiscoverWithNetworkScan(streamCtx, r.Msg.GetNetworkScan())
		}
	case *pb.DiscoverRequest_Mdns:
		resultChan, err = h.pairingSvc.DiscoverWithMDNS(streamCtx, r.Msg.GetMdns())
	default:
		return fleeterror.NewInternalError("unsupported mode")
	}
	if err != nil {
		if err := reportServerScanError(streamCtx, fwd, err); err != nil {
			return err
		}
	}

	sourceErr := h.forwardDiscoverySources(streamCtx, info.OrganizationID, resultChan, nodeReq, fwd)
	if err := fwd.failure(); err != nil {
		return err
	}
	// Keep explicit cancellation quiet; caller deadlines remain transport errors.
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return connect.NewError(connect.CodeDeadlineExceeded, ctxErr)
		}
		return nil
	}
	if errors.Is(streamCtx.Err(), context.DeadlineExceeded) {
		return fwd.forwardTerminalWarning(streamCtx, discoverylimits.RequestTimeoutWarning)
	}
	return sourceErr
}

func (h *Handler) forwardDiscoverySources(
	ctx context.Context,
	organizationID int64,
	serverResults <-chan *pb.DiscoverResponse,
	nodeReq *pb.DiscoverRequest,
	fwd *dedupForwarder,
) error {
	ctx, cancelRequest := discovery.NewRequestContext(ctx)
	defer cancelRequest()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var sourceErr error
	var failOnce sync.Once
	fail := func(err error) {
		failOnce.Do(func() { sourceErr = err; cancel() })
	}
	var wg sync.WaitGroup
	forwardServer := func(serverResults <-chan *pb.DiscoverResponse) {
		if serverResults == nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := forwardServerDiscoveryResults(ctx, serverResults, fwd, discoveryLookupBatchMaxDelay); err != nil && ctx.Err() == nil {
				fail(err)
			}
		}()
	}

	forwardServer(serverResults)

	if nodeReq != nil && h.discovery != nil {
		nodeIDs, err := h.discovery.DiscoveryNodeIDs(ctx, organizationID)
		if err != nil {
			if ctx.Err() == nil {
				if discovery.IsCallerError(err) {
					fail(err)
				} else {
					slog.Warn("skipping fleet node discovery fan-out", "error", err)
					// The server scan can continue even when the node directory is unavailable.
					_ = fwd.forward(ctx, &pb.DiscoverResponse{Warning: "Fleet Nodes: unable to list connected nodes"})
				}
			}
		} else {
			if serverResults == nil && len(nodeIDs) == 0 && nodeReq.GetNetworkScan() != nil &&
				discovery.ScansLocalSubnet(nodeReq.GetNetworkScan()) {
				results, scanErr := h.pairingSvc.DiscoverWithNetworkScan(ctx, nodeReq.GetNetworkScan())
				if scanErr == nil {
					forwardServer(results)
				} else if err := reportServerScanError(ctx, fwd, scanErr); err != nil {
					fail(err)
				}
			}
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
					runErr := h.discovery.RunOnNode(ctx, nodeID, "Fleet Node", nodeReq, func(response *pb.DiscoverResponse) error {
						return fwd.forward(ctx, response)
					})
					// Node target policy is narrower than the server's: public
					// addresses can still be scanned locally.
					// Direct-node requests retain their strict validation errors.
					if runErr == nil || ctx.Err() != nil {
						return
					}
					if !fleeterror.IsInvalidArgumentError(runErr) || fwd.failure() != nil {
						fail(runErr)
						return
					}
					if sendErr := fwd.forward(ctx, discovery.SourceWarning("Fleet Node", runErr)); sendErr != nil {
						fail(sendErr)
					}
				}(nodeID)
			}
		}
	}

	wg.Wait()
	return sourceErr
}

func forwardServerDiscoveryResults(
	ctx context.Context,
	results <-chan *pb.DiscoverResponse,
	fwd *dedupForwarder,
	maxDelay time.Duration,
) error {
	pending := make([]*pb.Device, 0, discoveryLookupBatchSize)
	var timer *time.Timer
	var timerC <-chan time.Time
	stopTimer := func() {
		if timer != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerC = nil
	}
	defer stopTimer()
	startTimer := func() bool {
		delay := maxDelay
		if deadline, ok := ctx.Deadline(); ok {
			timeBeforeDeadlineFlush := time.Until(deadline) - maxDelay
			if timeBeforeDeadlineFlush <= 0 {
				return false
			}
			delay = min(delay, timeBeforeDeadlineFlush)
		}
		if timer == nil {
			timer = time.NewTimer(delay)
		} else {
			timer.Reset(delay)
		}
		timerC = timer.C
		return true
	}
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		stopTimer()
		devices := pending
		pending = make([]*pb.Device, 0, discoveryLookupBatchSize)
		return fwd.forward(ctx, &pb.DiscoverResponse{Devices: devices})
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("server discovery batching stopped: %w", ctx.Err())
		case <-timerC:
			timerC = nil
			if err := flush(); err != nil {
				return err
			}
		case response, ok := <-results:
			if !ok {
				return flush()
			}
			if response.GetWarning() != "" || len(response.GetDevices()) == 0 {
				if err := flush(); err != nil {
					return err
				}
				if err := fwd.forward(ctx, response); err != nil {
					return err
				}
				continue
			}
			for _, device := range response.GetDevices() {
				pending = append(pending, device)
				if len(pending) == 1 && !startTimer() {
					if err := flush(); err != nil {
						return err
					}
					continue
				}
				if len(pending) == discoveryLookupBatchSize {
					if err := flush(); err != nil {
						return err
					}
				}
			}
		}
	}
}

// reportServerScanError returns caller errors and forwards other Fleet Server
// scan failures as a warning unless the request has ended.
func reportServerScanError(ctx context.Context, fwd *dedupForwarder, err error) error {
	if discovery.IsCallerError(err) {
		return err
	}
	if ctx.Err() == nil {
		slog.Warn("fleet server discovery failed", "error", err)
		return fwd.forward(ctx, discovery.SourceWarning("Fleet Server", err))
	}
	return nil
}

// fleetNodeDiscoveryRequest returns requests supported by Fleet Nodes. The
// discovery service translates automatic local-subnet scans before dispatch.
func fleetNodeDiscoveryRequest(req *pb.DiscoverRequest) *pb.DiscoverRequest {
	switch req.GetMode().(type) {
	case *pb.DiscoverRequest_IpList, *pb.DiscoverRequest_IpRange, *pb.DiscoverRequest_NetworkScan:
		return req
	default:
		return nil
	}
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

	nodeIDs, err := h.discovery.DiscoveryNodeIDs(ctx, orgID)
	if err != nil {
		slog.Warn("skipping fleet node pairing route", "error", err)
		return resp, route, nil
	}
	if len(nodeIDs) == 0 {
		return resp, route, nil
	}
	nodeIDs = slices.Sorted(slices.Values(nodeIDs))

	ctx, cancel := context.WithTimeout(ctx, pairFleetNodeDeadline)
	defer cancel()

	type nodeWork struct {
		nodeID  int64
		targets []*pb.FleetNodePairTarget
		route   fleetNodePairRoute
		failed  map[string]struct{}
	}
	pending := setFromSlice(selected)
	sortedSelected := sortedKeys(pending)
	work := make([]nodeWork, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		item := nodeWork{nodeID: nodeID, route: fleetNodePairRoute{
			routedExplicit: map[string]struct{}{}, routedAllDevices: map[string]struct{}{},
		}, failed: map[string]struct{}{}}
		if allDevicesFilter == nil {
			targetIDs := pendingSortedIdentifiers(sortedSelected, pending)
			for ids := range slices.Chunk(targetIDs, pairLookupBatchSize) {
				if err := ctx.Err(); err != nil {
					return nil, fleetNodePairRoute{}, fmt.Errorf("pair stopped before resolving next batch: %w", err)
				}
				targets, err := h.fleetNodePairing.ResolvePairTargets(ctx, nodeID, orgID, ids, false, req.GetCredentials())
				if err != nil {
					return nil, fleetNodePairRoute{}, err
				}
				for _, target := range targets {
					delete(pending, target.GetDeviceIdentifier())
				}
				item.targets = append(item.targets, targets...)
				recordFleetNodePairTargets(targets, false, item.route.routedExplicit, item.route.routedAllDevices, item.failed)
			}
			if len(item.targets) == 0 {
				continue
			}
		}
		work = append(work, item)
		if allDevicesFilter == nil && len(pending) == 0 {
			break
		}
	}

	var wg sync.WaitGroup
	for i := range work {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item := &work[i]
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in fleet node pairing worker", "fleet_node_id", item.nodeID, "panic", r)
					if item.route.remoteErr == nil {
						item.route.remoteErr = fleeterror.NewInternalError("fleet node pairing worker panicked")
					}
				}
			}()
			if allDevicesFilter != nil {
				if err := h.pairAllMatchingFleetNodeDevices(ctx, item.nodeID, orgID, userID, allDevicesFilter, req, &item.route, item.failed); err != nil {
					slog.Warn("fleet node pair-all stopped", "fleet_node_id", item.nodeID, "error", err)
					item.route.allDevices = true
					if item.route.remoteErr == nil {
						item.route.remoteErr = err
					}
				}
				return
			}
			for targets := range slices.Chunk(item.targets, fleetnodepairing.MaxPairBatch) {
				if err := ctx.Err(); err != nil {
					item.route.remoteErr = err
					return
				}
				if err := h.pairFleetNodeTargetBatch(ctx, item.nodeID, orgID, userID, req, targets, false, &item.route, item.failed); err != nil {
					slog.Warn("fleet node pairing command failed; returning failed targets in pair response", "fleet_node_id", item.nodeID, "error", err)
					if item.route.remoteErr == nil {
						item.route.remoteErr = err
					}
					return
				}
			}
		}()
	}
	wg.Wait()
	failed := map[string]struct{}{}
	for _, item := range work {
		for id := range item.route.routedExplicit {
			route.routedExplicit[id] = struct{}{}
		}
		for id := range item.route.routedAllDevices {
			route.routedAllDevices[id] = struct{}{}
		}
		for id := range item.failed {
			failed[id] = struct{}{}
		}
		route.allDevices = route.allDevices || item.route.allDevices
		route.remoteSucceeded = route.remoteSucceeded || item.route.remoteSucceeded
		if route.remoteErr == nil {
			route.remoteErr = item.route.remoteErr
		}
	}
	resp.FailedDeviceIds = sortedKeys(failed)
	if allDevicesFilter != nil && route.remoteErr != nil {
		return resp, route, route.remoteErr
	}
	return resp, route, nil
}

func (h *Handler) pairAllMatchingFleetNodeDevices(ctx context.Context, nodeID, orgID, userID int64, allDevicesFilter *minercommandv1.DeviceFilter, req *pb.PairRequest, route *fleetNodePairRoute, failed map[string]struct{}) error {
	var cursorID *int64
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("pair stopped before next page: %w", err)
		}
		targets, nextCursor, err := h.fleetNodePairing.ResolvePairTargetsByFilterPage(ctx, nodeID, orgID, allDevicesFilter, req.GetCredentials(), cursorID)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return nil
		}
		recordFleetNodePairTargets(targets, true, route.routedExplicit, route.routedAllDevices, failed)
		if err := h.pairFleetNodeTargetBatch(ctx, nodeID, orgID, userID, req, targets, true, route, failed); err != nil {
			return err
		}
		cursorID = nextCursor
		if cursorID == nil {
			return nil
		}
	}
}

func recordFleetNodePairTargets(targets []*pb.FleetNodePairTarget, allDevices bool, routedExplicit, routedAllDevices, failed map[string]struct{}) {
	for _, target := range targets {
		id := target.GetDeviceIdentifier()
		if allDevices {
			routedAllDevices[id] = struct{}{}
		} else {
			routedExplicit[id] = struct{}{}
		}
		failed[id] = struct{}{}
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
