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
	// nodes so their LAN-local miners surface alongside the cloud's own scan.
	// Optional; nil disables fan-out (cloud-only discovery).
	discovery *discovery.Service
}

// maxConcurrentFleetNodeScans bounds how many fleet nodes one fan-out scans at
// once, so a large fleet can't spawn an unbounded number of in-flight
// ControlStream commands (each held until its ack or the per-node timeout). It
// sits comfortably above typical fleet sizes; only the pathological case binds.
const maxConcurrentFleetNodeScans = 32

// fleetNodeFanOutTimeout caps how long the fleet-node fan-out can extend the
// Discover stream. RunOnNode's 12m timeout is the dedicated single-node budget;
// for the opportunistic fan-out a tighter ceiling keeps a wedged node from making
// the operator wait minutes past the cloud scan. A LAN subnet scan finishes well
// within this.
const fleetNodeFanOutTimeout = 5 * time.Minute

var _ pairingv1connect.PairingServiceHandler = &Handler{}

// NewHandler creates a new instance of Handler
func NewHandler(pairingSvc *pairing.Service, discoverySvc *discovery.Service) *Handler {
	return &Handler{
		pairingSvc: pairingSvc,
		discovery:  discoverySvc,
	}
}

// Discover implements pairingv1connect.PairingServiceHandler.
//
// Beyond the cloud's own network scan, an nmap ("Scan your network") request
// also fans out to every CONFIRMED + connected fleet node, which scan their own
// local subnets and report back. All sources merge into this single response
// stream so the operator pairs LAN-local and cloud-local miners together with no
// client change. Manual modes (ipList/ipRange/mdns) target the cloud's own
// network only.
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

	// Fleet node fan-out, only for the automatic "Scan your network" action — an
	// nmap target equal to the cloud's own local subnet (reported by
	// DiscoverWithNmap). A manual/explicit nmap target must NOT sweep every node's
	// LAN.
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
					// One node failing (or hitting the fan-out budget) must not
					// fail the scan; it's expected on disconnect/budget, so stay
					// quiet once fanOutCtx is done.
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
	// A client cancel/deadline drains the sources without a Send error; surface
	// it as canceled/deadline rather than a successful completion. (The fan-out
	// budget firing is not a client error — it returns whatever streamed.)
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
