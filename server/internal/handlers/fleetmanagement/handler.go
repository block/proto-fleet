package fleetmanagement

import (
	"context"

	"connectrpc.com/connect"
	"crypto/tls"
	"fmt"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	minerPb "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api/miner_command_apiconnect"
	minerPbCommon "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement"
	"golang.org/x/net/http2"
	"net"
	"net/http"
	"strings"
)

// Handler handles the Connect-RPC endpoints
type Handler struct {
	fleetMgmtSvc *fleetmanagement.Service
}

var _ fleetmanagementv1connect.FleetManagementServiceHandler = &Handler{}

func NewHandler(fleetMgmtSvc *fleetmanagement.Service) *Handler {
	return &Handler{fleetMgmtSvc: fleetMgmtSvc}
}

func (h *Handler) SetDefaultPool(ctx context.Context, r *connect.Request[pb.SetDefaultPoolRequest]) (*connect.Response[pb.SetDefaultPoolResponse], error) {
	err := h.fleetMgmtSvc.UpdateDefaultPool(ctx, r.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &connect.Response[pb.SetDefaultPoolResponse]{}, nil
}

// ListPairedMiners implements fleetmanagementv1connect.FleetManagementServiceHandler.
func (h *Handler) ListPairedMiners(ctx context.Context, r *connect.Request[pb.ListPairedMinersRequest]) (*connect.Response[pb.ListPairedMinersResponse], error) {
	result, err := h.fleetMgmtSvc.ListPairedMiners(ctx, r.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) StopMining(
	ctx context.Context,
	req *connect.Request[pb.StopMiningRequest],
) (*connect.Response[pb.StopMiningResponse], error) {
	// TODO This code is just a placeholder for demonstrating calling the Miner API - replace with real implementation
	// Create a Connect client for the miner
	minerClient := minerPb.NewMinerCommandApiClient(
		&http.Client{
			Transport: &http2.Transport{
				AllowHTTP: true,
				DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
					// Check if the URL scheme is HTTPS
					if strings.HasPrefix(req.Msg.MinerUrl, "https://") {
						// Use tls.Dial for secure HTTPS connections
						return tls.Dial(network, addr, cfg)
					}

					// Otherwise, proceed with a plain connection for HTTP
					return net.Dial(network, addr)
				},
			},
		},
		req.Msg.MinerUrl,
		connect.WithGRPC(),
	)

	// Call the StopMining endpoint on the miner
	minerResp, err := minerClient.StopMining(ctx, connect.NewRequest(&minerPbCommon.EmptyRequest{}))
	if err != nil {
		return connect.NewResponse(&pb.StopMiningResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to stop mining: %v", err),
		}), nil
	}

	// Check the response from the miner
	if minerResp.Msg.Result != minerPbCommon.ApiResult_RESULT_SUCCESS {
		return connect.NewResponse(&pb.StopMiningResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Miner returned error: %s", minerResp.Msg.Message),
		}), nil
	}

	return connect.NewResponse(&pb.StopMiningResponse{
		Success:      true,
		ErrorMessage: "",
	}), nil
}
