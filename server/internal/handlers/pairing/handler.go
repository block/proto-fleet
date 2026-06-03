package pairing

import (
	"context"
	"errors"
	"log/slog"
	"sync"

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

	// Connect server streams are not safe for concurrent Send, and the cloud
	// scan + each node write to this one stream. Serialize through send, which
	// also dedupes devices across sources by identifier (each source dedupes
	// internally, but not against the others).
	var (
		sendMu  sync.Mutex
		seen    = make(map[string]struct{})
		sendErr error
	)
	send := func(resp *pb.DiscoverResponse) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		if sendErr != nil {
			return sendErr
		}
		out := resp
		if len(resp.GetDevices()) > 0 {
			deduped := make([]*pb.Device, 0, len(resp.GetDevices()))
			for _, d := range resp.GetDevices() {
				key := pairing.DeviceDedupKey(d)
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				deduped = append(deduped, d)
			}
			if len(deduped) == 0 && resp.GetError() == "" {
				return nil // whole batch was duplicates; nothing to forward
			}
			if len(deduped) < len(resp.GetDevices()) {
				out = &pb.DiscoverResponse{Devices: deduped, Error: resp.GetError()}
			}
		}
		if sErr := s.Send(out); sErr != nil {
			sendErr = sErr
			cancel()
			return sErr //nolint:wrapcheck // a connect stream Send error is already a connect error
		}
		return nil
	}

	var resultChan <-chan *pb.DiscoverResponse
	var isNmap bool
	switch r.Msg.Mode.(type) {
	case *pb.DiscoverRequest_IpList:
		resultChan, err = h.pairingSvc.DiscoverWithIPList(streamCtx, r.Msg.GetIpList())
	case *pb.DiscoverRequest_IpRange:
		resultChan, err = h.pairingSvc.DiscoverWithIPRange(streamCtx, r.Msg.GetIpRange())
	case *pb.DiscoverRequest_Nmap:
		isNmap = true
		resultChan, err = h.pairingSvc.DiscoverWithNmap(streamCtx, r.Msg.GetNmap())
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
				if err := send(result); err != nil {
					return
				}
			case <-streamCtx.Done():
				return
			}
		}
	}()

	// Fleet node fan-out, gated to the automatic "Scan your network" action: an
	// nmap request whose target is the cloud's own local subnet. A manual/explicit
	// nmap target (a user-typed subnet/IP) must NOT also sweep every node's LAN.
	if isNmap && h.discovery != nil &&
		h.pairingSvc.IsLocalSubnetScan(streamCtx, r.Msg.GetNmap().GetTarget()) {
		nodeIDs, listErr := h.discovery.ConfirmedConnectedNodeIDs(streamCtx, info.OrganizationID)
		if listErr != nil {
			// Fan-out is best-effort; a lookup failure must never break the
			// cloud scan. With zero connected nodes this is the same path.
			slog.Warn("skipping fleet node discovery fan-out", "error", listErr)
		} else {
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
					// operator disconnected while we were queued behind the cap.
					select {
					case sem <- struct{}{}:
						defer func() { <-sem }()
					case <-streamCtx.Done():
						return
					}
					runErr := h.discovery.RunOnNode(streamCtx, nodeID, autoReq, send)
					// One node failing must not fail the whole scan. Stay quiet
					// when streamCtx is already cancelled (operator disconnected) —
					// that's expected, not a node fault.
					if runErr != nil && streamCtx.Err() == nil {
						slog.Warn("fleet node discovery failed during cloud fan-out",
							"fleet_node_id", nodeID, "error", runErr)
					}
				}(nodeID)
			}
		}
	}

	wg.Wait()
	if sendErr != nil {
		return sendErr
	}
	// A client cancel/deadline drains the sources without a Send error; surface
	// it as canceled/deadline rather than a successful completion.
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
