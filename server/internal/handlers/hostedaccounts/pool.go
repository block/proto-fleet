package hostedaccounts

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func poolOptions(input *pb.PoolRevisionInput) (domain.PoolRevisionOptions, error) {
	if input == nil {
		return domain.PoolRevisionOptions{}, fleeterror.NewInvalidArgumentError("pool configuration required")
	}
	options := domain.PoolRevisionOptions{Name: input.Name, Slots: make([]domain.PoolSlotOptions, 0, len(input.Slots))}
	for _, slot := range input.Slots {
		if slot == nil {
			return domain.PoolRevisionOptions{}, fleeterror.NewInvalidArgumentError("pool slot required")
		}
		options.Slots = append(options.Slots, domain.PoolSlotOptions{URL: slot.Url, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate, Password: slot.Password})
	}
	return options, nil
}
func poolRevisionDTO(revision domain.PoolRevision) *pb.PoolRevision {
	result := &pb.PoolRevision{PoolId: revision.PoolID, RevisionId: revision.RevisionID, Number: revision.Number, Name: revision.Name, CreatedAt: timestamppb.New(revision.CreatedAt), Archived: revision.Archived}
	for _, slot := range revision.Slots {
		result.Slots = append(result.Slots, &pb.PoolEndpoint{Priority: slot.Priority, Url: slot.URL, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate, HasPassword: slot.HasPassword})
	}
	return result
}
func (h *Handler) CreatePoolConfiguration(ctx context.Context, req *connect.Request[pb.CreatePoolConfigurationRequest]) (*connect.Response[pb.CreatePoolConfigurationResponse], error) {
	info, err := readPrincipal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	options, err := poolOptions(req.Msg.Configuration)
	if err != nil {
		return nil, err
	}
	revision, err := h.pools.Create(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, options)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreatePoolConfigurationResponse{Revision: poolRevisionDTO(revision)}), nil
}
func (h *Handler) RevisePoolConfiguration(ctx context.Context, req *connect.Request[pb.RevisePoolConfigurationRequest]) (*connect.Response[pb.RevisePoolConfigurationResponse], error) {
	info, err := readPrincipal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	options, err := poolOptions(req.Msg.Configuration)
	if err != nil {
		return nil, err
	}
	revision, err := h.pools.Revise(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.PoolId, req.Msg.ExpectedRevisionId, options)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevisePoolConfigurationResponse{Revision: poolRevisionDTO(revision)}), nil
}
func (h *Handler) GetPoolRevision(ctx context.Context, req *connect.Request[pb.GetPoolRevisionRequest]) (*connect.Response[pb.GetPoolRevisionResponse], error) {
	info, err := readPrincipal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	revision, err := h.pools.Get(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.PoolId, req.Msg.GetRevisionId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetPoolRevisionResponse{Revision: poolRevisionDTO(revision)}), nil
}
func (h *Handler) ListPoolConfigurations(ctx context.Context, req *connect.Request[pb.ListPoolConfigurationsRequest]) (*connect.Response[pb.ListPoolConfigurationsResponse], error) {
	info, err := readPrincipal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	if req.Msg.PageSize > 100 {
		return nil, fleeterror.NewInvalidArgumentError("page size must be at most 100")
	}
	page, err := h.pools.List(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.PageToken, int32(req.Msg.PageSize), req.Msg.IncludeArchived) // #nosec G115 -- page size is checked above to be at most 100.
	if err != nil {
		return nil, err
	}
	result := &pb.ListPoolConfigurationsResponse{NextPageToken: page.NextPageToken}
	for _, revision := range page.Configurations {
		result.Configurations = append(result.Configurations, poolRevisionDTO(revision))
	}
	return connect.NewResponse(result), nil
}
func (h *Handler) ArchivePoolConfiguration(ctx context.Context, req *connect.Request[pb.ArchivePoolConfigurationRequest]) (*connect.Response[pb.ArchivePoolConfigurationResponse], error) {
	info, err := readPrincipal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	if err = h.pools.Archive(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.PoolId); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ArchivePoolConfigurationResponse{}), nil
}
func (h *Handler) GetIncomingPoolProfile(ctx context.Context, req *connect.Request[pb.GetIncomingPoolProfileRequest]) (*connect.Response[pb.GetIncomingPoolProfileResponse], error) {
	info, err := readPrincipal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	profile, err := h.pools.IncomingProfile(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	result := &pb.GetIncomingPoolProfileResponse{}
	if profile.Revision > 0 {
		result.Profile = &pb.IncomingPoolProfile{RevisionId: profile.RevisionID, Revision: profile.Revision, UpdatedAt: timestamppb.New(profile.UpdatedAt)}
	}
	return connect.NewResponse(result), nil
}
func (h *Handler) SetIncomingPoolProfile(ctx context.Context, req *connect.Request[pb.SetIncomingPoolProfileRequest]) (*connect.Response[pb.SetIncomingPoolProfileResponse], error) {
	info, err := readPrincipal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	if err = h.pools.SetIncomingProfile(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.GetRevisionId(), req.Msg.Password); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetIncomingPoolProfileResponse{}), nil
}
