package pools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/pools"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
)

type Handler struct {
	poolsSvc *pools.Service
}

var _ poolsv1connect.PoolsServiceHandler = &Handler{}

func NewHandler(svc *pools.Service) *Handler {
	return &Handler{
		poolsSvc: svc,
	}
}

func (h *Handler) ListPools(ctx context.Context, _ *connect.Request[pb.ListPoolsRequest]) (*connect.Response[pb.ListPoolsResponse], error) {
	listedPools, err := h.poolsSvc.ListPools(ctx)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.ListPoolsResponse{Pools: listedPools}), nil
}

func (h *Handler) CreatePool(ctx context.Context, r *connect.Request[pb.CreatePoolRequest]) (*connect.Response[pb.CreatePoolResponse], error) {
	pool, err := h.poolsSvc.CreatePool(ctx, r.Msg.PoolConfig)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.CreatePoolResponse{Pool: pool}), nil
}

func (h *Handler) UpdatePool(ctx context.Context, r *connect.Request[pb.UpdatePoolRequest]) (*connect.Response[pb.UpdatePoolResponse], error) {
	pool, err := h.poolsSvc.UpdatePool(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.UpdatePoolResponse{Pool: pool}), nil
}

func (h *Handler) DeletePool(ctx context.Context, r *connect.Request[pb.DeletePoolRequest]) (*connect.Response[pb.DeletePoolResponse], error) {
	err := h.poolsSvc.DeletePool(ctx, r.Msg.PoolId)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.DeletePoolResponse{}), nil
}

func (h *Handler) ValidatePool(ctx context.Context, r *connect.Request[pb.ValidatePoolRequest]) (*connect.Response[pb.ValidatePoolResponse], error) {
	var pass *secrets.Text
	if r.Msg.Password != nil {
		pass = secrets.NewText(r.Msg.Password.GetValue())
	}

	var timeout *time.Duration
	if r.Msg.Timeout != nil {
		tmp := r.Msg.Timeout.AsDuration()
		timeout = &tmp
	}

	// Forward the typed result as-is: Reachable / CredentialsVerified /
	// Mode let the UI render "reachable but credentials unverified" (v1
	// SV2 default) without guessing from the pair (protocol, success).
	// Network-level failures (timeout, DNS, RST) still return a gRPC
	// error; the success path with !Reachable would be unreachable but
	// we keep the field for symmetry and future modes.
	result, err := h.poolsSvc.ValidateConnection(ctx, r.Msg.Url, r.Msg.Username, pass, r.Msg.NoisePublicKey, timeout)
	if err != nil {
		// Preserve the underlying error semantics: validation errors
		// (bad URL scheme, malformed Noise key) come back as FleetError
		// with their gRPC code already set; everything else is a probe
		// failure (TCP refused, DNS, SV1 auth rejection) that maps to
		// Unavailable. Lumping all of these into PermissionDenied loses
		// information the operator UI uses to render distinct error
		// states.
		var fe fleeterror.FleetError
		if errors.As(err, &fe) {
			return nil, fe.ConnectError()
		}
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to validate pool connection: %w", err))
	}
	if !result.Reachable {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("pool unreachable"))
	}
	// Preserve the pre-v1 error contract for SV1 authentication
	// failures: stale clients (browser bundles cached across an
	// upgrade, third-party tooling) treat any 200 OK as "validation
	// succeeded" and would silently accept invalid credentials with
	// the new typed-success response. SV2 paths can't be authenticated
	// in v1 (TCP_DIAL has nothing to verify; HANDSHAKE proves identity
	// pinning, not credentials) so a !CredentialsVerified outcome
	// there isn't a credential failure — return the typed success
	// body. SV1 keeps the non-OK status for backward compat.
	if result.Mode == pb.ValidationMode_VALIDATION_MODE_SV1_AUTHENTICATE && !result.CredentialsVerified {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("pool authentication failed"))
	}

	return connect.NewResponse(&pb.ValidatePoolResponse{
		Reachable:           result.Reachable,
		CredentialsVerified: result.CredentialsVerified,
		Mode:                result.Mode,
	}), nil
}
