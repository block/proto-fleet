package pairing

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/discovery"
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
	// discovery fans the "Scan your network" nmap action out to connected fleet
	// nodes; nil disables fan-out (cloud-only discovery).
	discovery *discovery.Service
}

// maxConcurrentFleetNodeScans bounds in-flight per-node commands so a large fleet
// can't spawn an unbounded number of ControlStream slots. Above typical fleet sizes.
const maxConcurrentFleetNodeScans = 32

// fleetNodeFanOutTimeout caps how long the opportunistic fan-out can extend the
// Discover stream — tighter than RunOnNode's 12m budget so one wedged node can't
// make the operator wait minutes past the cloud scan.
const fleetNodeFanOutTimeout = 5 * time.Minute

var _ pairingv1connect.PairingServiceHandler = &Handler{}

// NewHandler creates a new instance of Handler
func NewHandler(pairingSvc *pairing.Service, discoverySvc *discovery.Service) *Handler {
	return &Handler{
		pairingSvc: pairingSvc,
		discovery:  discoverySvc,
	}
}

// Discover implements pairingv1connect.PairingServiceHandler. An nmap "Scan your
// network" request also fans out to every CONFIRMED + connected fleet node and
// merges their LAN-local results into this stream; other modes are cloud-only.
func (h *Handler) Discover(ctx context.Context, r *connect.Request[pb.DiscoverRequest], s *connect.ServerStream[pb.DiscoverResponse]) error {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerPair, authz.ResourceContext{})
	if err != nil {
		return err
	}
	slog.Debug("Discover: handling discover request", "payload", r.Msg)

	// A send failure (operator disconnected) cancels every source.
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Serialize the concurrent sources (cloud scan + each node) onto the one
	// stream and dedupe devices across them; a Send failure cancels the rest.
	fwd := newDedupForwarder(s.Send, cancel)

	var resultChan <-chan *pb.DiscoverResponse
	var isLocalSubnetNmap bool
	switch r.Msg.Mode.(type) {
	case *pb.DiscoverRequest_IpList:
		resultChan, err = h.pairingSvc.DiscoverWithIPList(streamCtx, r.Msg.GetIpList())
	case *pb.DiscoverRequest_IpRange:
		resultChan, err = h.pairingSvc.DiscoverWithIPRange(streamCtx, r.Msg.GetIpRange())
	case *pb.DiscoverRequest_Nmap:
		resultChan, isLocalSubnetNmap, err = h.pairingSvc.DiscoverWithNmap(streamCtx, r.Msg.GetNmap())
	case *pb.DiscoverRequest_Mdns:
		resultChan, err = h.pairingSvc.DiscoverWithMDNS(streamCtx, r.Msg.GetMdns())
	default:
		return fleeterror.NewInternalError("unsupported mode")
	}
	if err != nil {
		return err
	}

	var wg sync.WaitGroup

	// Cloud discovery source.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case result, ok := <-resultChan:
				if !ok {
					return
				}
				if err := fwd.forward(result); err != nil {
					return
				}
			case <-streamCtx.Done():
				return
			}
		}
	}()

	// Fan out only for the automatic "Scan your network" action (nmap target ==
	// the cloud's own local subnet), never a manual/explicit target.
	if isLocalSubnetNmap && h.discovery != nil {
		nodeIDs, listErr := h.discovery.ConfirmedConnectedNodeIDs(streamCtx, info.OrganizationID)
		if listErr != nil {
			// Fan-out is best-effort; a lookup failure must never break the
			// cloud scan. With zero connected nodes this is the same path.
			slog.Warn("skipping fleet node discovery fan-out", "error", listErr)
		} else {
			// Bound the fan-out's contribution to the stream so one wedged node
			// can't extend the operator's wait to the full per-node timeout.
			fanOutCtx, fanOutCancel := context.WithTimeout(streamCtx, fleetNodeFanOutTimeout)
			defer fanOutCancel()
			autoReq := &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_Nmap{Nmap: &pb.NmapModeRequest{
				Target: nmaptarget.LocalSubnetTarget,
				Ports:  r.Msg.GetNmap().GetPorts(),
			}}}
			sem := make(chan struct{}, maxConcurrentFleetNodeScans)
			for _, nodeID := range nodeIDs {
				wg.Add(1)
				go func(nodeID int64) {
					defer wg.Done()
					// Cap concurrent in-flight node commands; exit early if the
					// stream closed or the fan-out budget expired while queued.
					select {
					case sem <- struct{}{}:
						defer func() { <-sem }()
					case <-fanOutCtx.Done():
						return
					}
					runErr := h.discovery.RunOnNode(fanOutCtx, nodeID, autoReq, fwd.forward)
					// One node failing must not fail the scan, and is expected
					// once fanOutCtx is done (disconnect/budget) — stay quiet then.
					if runErr != nil && fanOutCtx.Err() == nil {
						slog.Warn("fleet node discovery failed during cloud fan-out",
							"fleet_node_id", nodeID, "error", runErr)
					}
				}(nodeID)
			}
		}
	}

	wg.Wait()
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

// Pair implements pairingv1connect.PairingServiceHandler.
func (h *Handler) Pair(ctx context.Context, r *connect.Request[pb.PairRequest]) (*connect.Response[pb.PairResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerPair, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	resp, err := h.pairingSvc.PairDevices(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}
