package apikey

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/apikey/v1"
	"github.com/block/proto-fleet/server/generated/grpc/apikey/v1/apikeyv1connect"
	domainApiKey "github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler handles API key management requests.
type Handler struct {
	apiKeySvc *domainApiKey.Service
}

var _ apikeyv1connect.ApiKeyServiceHandler = &Handler{}

// NewHandler creates a new API key handler.
func NewHandler(apiKeySvc *domainApiKey.Service) *Handler {
	return &Handler{apiKeySvc: apiKeySvc}
}

func (h *Handler) CreateApiKey(ctx context.Context, req *connect.Request[pb.CreateApiKeyRequest]) (*connect.Response[pb.CreateApiKeyResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermAPIKeyManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}

	var expiresAt *time.Time
	if req.Msg.ExpiresAt != nil {
		t := req.Msg.ExpiresAt.AsTime()
		expiresAt = &t
	}

	fullKey, apiKey, err := h.apiKeySvc.Create(ctx, info.UserID, info.OrganizationID, info.ExternalUserID, info.Username, req.Msg.Name, expiresAt)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.CreateApiKeyResponse{
		ApiKey: fullKey,
		Info:   apiKeyToProto(apiKey),
	}), nil
}

func (h *Handler) ListApiKeys(ctx context.Context, _ *connect.Request[pb.ListApiKeysRequest]) (*connect.Response[pb.ListApiKeysResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermAPIKeyManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}

	keys, err := h.apiKeySvc.List(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	protoKeys := make([]*pb.ApiKeyInfo, 0, len(keys))
	for i := range keys {
		protoKeys = append(protoKeys, apiKeyToProto(&keys[i]))
	}

	return connect.NewResponse(&pb.ListApiKeysResponse{
		ApiKeys: protoKeys,
	}), nil
}

func (h *Handler) RevokeApiKey(ctx context.Context, req *connect.Request[pb.RevokeApiKeyRequest]) (*connect.Response[pb.RevokeApiKeyResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermAPIKeyManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}

	if err := h.apiKeySvc.Revoke(ctx, req.Msg.KeyId, info.OrganizationID, info.ExternalUserID, info.Username); err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.RevokeApiKeyResponse{}), nil
}

func apiKeyToProto(key *interfaces.ApiKey) *pb.ApiKeyInfo {
	info := &pb.ApiKeyInfo{
		KeyId:     key.KeyID,
		Name:      key.Name,
		Prefix:    fmt.Sprintf("fleet_%s", key.Prefix),
		CreatedAt: timestamppb.New(key.CreatedAt),
		CreatedBy: key.CreatedByUsername,
	}
	if key.ExpiresAt != nil {
		info.ExpiresAt = timestamppb.New(*key.ExpiresAt)
	}
	if key.LastUsedAt != nil {
		info.LastUsedAt = timestamppb.New(*key.LastUsedAt)
	}
	return info
}
