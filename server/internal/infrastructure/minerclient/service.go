package minerclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
)

// Service handles miner gRPC client operations
type Service struct {
	httpClient  *http.Client
	httpsClient *http.Client
}

// NewService creates a new miner client service instance
func NewService() *Service {
	return &Service{
		httpClient: &http.Client{
			Transport: &http2.Transport{
				AllowHTTP: true,
				DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
					return net.Dial(network, addr)
				},
			},
		},
		httpsClient: http.DefaultClient,
	}
}

// Request represents a gRPC request with its client factory and call function
type Request[ReqDTO any, RespDTO any, Client any] struct {
	ClientFactory func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) Client
	RPCCall       func(client Client, ctx context.Context, connectReq *connect.Request[ReqDTO]) (*connect.Response[RespDTO], error)
	RequestDTO    *ReqDTO
}

// Execute executes a gRPC call on a miner with automatic protocol fallback
func Execute[Req any, Resp any, Client any](
	ctx context.Context,
	s *Service,
	minerURL string,
	rpcReq Request[Req, Resp, Client],
) (*connect.Response[Resp], error) {
	// TODO Check if the controlled miners belong to this user organization

	// Try HTTPS first
	response, err := executeWithProtocol(ctx, s.httpsClient, minerURL, rpcReq, "https")
	if err != nil {
		var errHTTP error

		// Fallback to HTTP
		response, errHTTP = executeWithProtocol(ctx, s.httpClient, minerURL, rpcReq, "http")
		if errHTTP != nil {
			return nil, fmt.Errorf("failed to execute gRPC call to: %s: HTTPS error: %w; HTTP error: %w", minerURL, err, errHTTP)
		}
	}

	return response, nil
}

// executeWithProtocol executes a gRPC call using the specified protocol
func executeWithProtocol[Req any, Resp any, Client any](
	ctx context.Context,
	httpClient *http.Client,
	minerURL string,
	rpcReq Request[Req, Resp, Client],
	protocol string,
) (*connect.Response[Resp], error) {
	baseURL := protocol + "://" + minerURL
	client := rpcReq.ClientFactory(httpClient, baseURL, connect.WithGRPC())

	connectRequest := connect.NewRequest(rpcReq.RequestDTO)

	resp, err := rpcReq.RPCCall(client, ctx, connectRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to execute gRPC call with %s: %w", protocol, err)
	}

	return resp, nil
}
