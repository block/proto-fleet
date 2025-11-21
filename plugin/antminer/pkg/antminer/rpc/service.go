package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"bufio"

	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/networking"
)

// timeouts and max response size to prevent large responses from causing issues
const (
	DefaultDialTimeout = 10 * time.Second
	DefaultReadTimeout = 30 * time.Second
	MaxResponseSize    = 1 << 20 // 1MB
)

//go:generate mockgen -source=service.go -destination=mocks/mock_rpc_client.go -package=mocks RPCClient
type RPCClient interface {
	GetSummary(ctx context.Context, connInfo *networking.ConnectionInfo) (*SummaryResponse, error)
	GetPools(ctx context.Context, connInfo *networking.ConnectionInfo) (*PoolsResponse, error)
	GetVersion(ctx context.Context, connInfo *networking.ConnectionInfo) (*VersionResponse, error)
	GetDevs(ctx context.Context, connInfo *networking.ConnectionInfo) (*DevsResponse, error)
	GetConfig(ctx context.Context, connInfo *networking.ConnectionInfo) (*ConfigResponse, error)
}

var _ RPCClient = &Service{}

type ServiceOption func(*Service)

func WithDialTimeout(timeout time.Duration) ServiceOption {
	return func(s *Service) {
		s.dialTimeout = timeout
	}
}

func WithReadTimeout(timeout time.Duration) ServiceOption {
	return func(s *Service) {
		s.readTimeout = timeout
	}
}

type Service struct {
	dialTimeout time.Duration
	readTimeout time.Duration
}

func NewService(opts ...ServiceOption) *Service {
	service := &Service{
		dialTimeout: DefaultDialTimeout,
		readTimeout: DefaultReadTimeout,
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func (s *Service) request(ctx context.Context, connInfo *networking.ConnectionInfo, cmd string, out any) error {
	req := &RPCRequest{Command: cmd}
	return s.executeRPCCommand(ctx, connInfo, req, out)
}

func (s *Service) executeRPCCommand(ctx context.Context, connInfo *networking.ConnectionInfo, request *RPCRequest, out any) error {
	address := connInfo.GetURL().Host
	protocol := connInfo.Protocol.String()
	dialer := &net.Dialer{Timeout: s.dialTimeout}

	conn, err := dialer.DialContext(ctx, protocol, address)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %v", address, err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(s.readTimeout)); err != nil {
		return fmt.Errorf("failed to set read deadline: %v", err)
	}

	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return fmt.Errorf("failed to encode request: %v", err)
	}

	// Use a limited reader to prevent reading more than MaxResponseSize
	limitReader := io.LimitReader(conn, MaxResponseSize)
	reader := bufio.NewReader(limitReader)
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()

	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	return nil
}

func (s *Service) GetSummary(ctx context.Context, connInfo *networking.ConnectionInfo) (*SummaryResponse, error) {
	var resp SummaryResponse
	if err := s.request(ctx, connInfo, "summary", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *Service) GetPools(ctx context.Context, connInfo *networking.ConnectionInfo) (*PoolsResponse, error) {
	var resp PoolsResponse
	if err := s.request(ctx, connInfo, "pools", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *Service) GetVersion(ctx context.Context, connInfo *networking.ConnectionInfo) (*VersionResponse, error) {
	var resp VersionResponse
	if err := s.request(ctx, connInfo, "version", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *Service) GetDevs(ctx context.Context, connInfo *networking.ConnectionInfo) (*DevsResponse, error) {
	var resp DevsResponse
	if err := s.request(ctx, connInfo, "devs", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *Service) GetConfig(ctx context.Context, connInfo *networking.ConnectionInfo) (*ConfigResponse, error) {
	var resp ConfigResponse
	if err := s.request(ctx, connInfo, "config", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
