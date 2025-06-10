package client

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"

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

func ExecuteWithoutAuth[Req any, Resp any, Client any](
	ctx context.Context,
	s *Service,
	url string,
	rpcReq Request[Req, Resp, Client],
) (*connect.Response[Resp], error) {
	response, err := executeWithProtocolWithoutAuth(ctx, s.httpsClient, url, rpcReq, "https")
	if err != nil {
		var errHTTP error

		response, errHTTP = executeWithProtocolWithoutAuth(ctx, s.httpClient, url, rpcReq, "http")
		if errHTTP != nil {
			return nil, fleeterror.NewInternalErrorf("failed to execute gRPC call to: %s, HTTPS error: %v; HTTP error: %v", url, err, errHTTP)
		}
	}

	return response, nil
}

// ExecuteWithAuth executes a gRPC call on a miner with automatic protocol fallback
func ExecuteWithAuth[Req any, Resp any, Client any](
	ctx context.Context,
	s *Service,
	minerConnectionInfo *MinerConnectionInfo,
	rpcReq Request[Req, Resp, Client],
) (*connect.Response[Resp], error) {
	// TODO Check if the controlled miners belong to this user organization

	URLCopy := *minerConnectionInfo.URL
	// Try HTTPS first
	URLCopy.Scheme = "https"
	response, err := executeWithProtocolWithAuth(ctx, s.httpsClient, URLCopy.String(), rpcReq, minerConnectionInfo.AuthToken)
	if err != nil {
		var errHTTP error

		// Fallback to HTTP
		URLCopy.Scheme = "http"
		response, errHTTP = executeWithProtocolWithAuth(ctx, s.httpClient, URLCopy.String(), rpcReq, minerConnectionInfo.AuthToken)
		if errHTTP != nil {
			return nil, fleeterror.NewInternalErrorf("failed to execute gRPC call to: %s with token: %s, HTTPS error: %v; HTTP error: %v", minerConnectionInfo.URL.String(), minerConnectionInfo.AuthToken, err, errHTTP)
		}
	}

	return response, nil
}

// executeWithProtocolWithAuth executes a gRPC call using the specified protocol
func executeWithProtocolWithAuth[Req any, Resp any, Client any](
	ctx context.Context,
	httpClient *http.Client,
	url string,
	rpcReq Request[Req, Resp, Client],
	authToken string,
) (*connect.Response[Resp], error) {
	client := rpcReq.ClientFactory(httpClient, url, connect.WithGRPC())

	connectRequest := connect.NewRequest(rpcReq.RequestDTO)

	connectRequest.Header().Set("Authorization", "Bearer "+authToken)

	resp, err := rpcReq.RPCCall(client, ctx, connectRequest)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to execute gRPC call to %s: %v", url, err)
	}

	return resp, nil
}

// executeWithProtocolWithAuth executes a gRPC call using the specified protocol
func executeWithProtocolWithoutAuth[Req any, Resp any, Client any](
	ctx context.Context,
	httpClient *http.Client,
	url string,
	rpcReq Request[Req, Resp, Client],
	protocol string,
) (*connect.Response[Resp], error) {
	baseURL := protocol + "://" + url
	client := rpcReq.ClientFactory(httpClient, baseURL, connect.WithGRPC())

	connectRequest := connect.NewRequest(rpcReq.RequestDTO)

	resp, err := rpcReq.RPCCall(client, ctx, connectRequest)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to execute gRPC call with %s: %v", protocol, err)
	}

	return resp, nil
}
