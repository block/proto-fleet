package poolconfigurations

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1/poolconfigurationsv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler struct{ service *domain.Service }

var _ poolconfigurationsv1connect.PoolConfigurationServiceHandler = (*Handler)(nil)

func NewHandler(service *domain.Service) *Handler { return &Handler{service: service} }

func inputOptions(input *pb.ConfigurationInput) (domain.RevisionOptions, error) {
	if input == nil {
		return domain.RevisionOptions{}, fleeterror.NewInvalidArgumentError("configuration is required")
	}
	result := domain.RevisionOptions{Name: input.Name, Slots: make([]domain.SlotOptions, 0, len(input.Pools))}
	for _, pool := range input.Pools {
		if pool == nil {
			return domain.RevisionOptions{}, fleeterror.NewInvalidArgumentError("pool is required")
		}
		result.Slots = append(result.Slots, domain.SlotOptions{ID: pool.SlotId, URL: pool.Url, Username: pool.Username, Password: pool.Password, WorkerTemplate: pool.WorkerTemplate})
	}
	return result, nil
}

func configurationDTO(c domain.Configuration, assignedCount int64) *pb.Configuration {
	result := &pb.Configuration{ConfigurationId: c.ID, Name: c.Name, RevisionId: c.RevisionID, Version: c.Version, Archived: c.Archived, AssignedCount: assignedCount}
	for _, pool := range c.Slots {
		result.Pools = append(result.Pools, &pb.Pool{SlotId: pool.ID, Url: pool.URL, Username: pool.Username, WorkerTemplate: pool.WorkerTemplate, HasPassword: pool.HasPassword})
	}
	return result
}

func (h *Handler) CreateConfiguration(ctx context.Context, req *connect.Request[pb.CreateConfigurationRequest]) (*connect.Response[pb.CreateConfigurationResponse], error) {
	options, err := inputOptions(req.Msg.Configuration)
	if err != nil {
		return nil, err
	}
	c, err := h.service.Create(ctx, req.Msg.AccountId, options)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateConfigurationResponse{Configuration: configurationDTO(c, 0)}), nil
}

func (h *Handler) GetConfiguration(ctx context.Context, req *connect.Request[pb.GetConfigurationRequest]) (*connect.Response[pb.GetConfigurationResponse], error) {
	c, err := h.service.Get(ctx, req.Msg.AccountId, req.Msg.ConfigurationId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetConfigurationResponse{Configuration: configurationDTO(c.Configuration, c.AssignedCount)}), nil
}

func (h *Handler) ListConfigurations(ctx context.Context, req *connect.Request[pb.ListConfigurationsRequest]) (*connect.Response[pb.ListConfigurationsResponse], error) {
	page, err := h.service.List(ctx, req.Msg.AccountId, req.Msg.PageToken, req.Msg.PageSize, req.Msg.IncludeArchived)
	if err != nil {
		return nil, err
	}
	result := &pb.ListConfigurationsResponse{NextPageToken: page.NextPageToken}
	for _, c := range page.Configurations {
		result.Configurations = append(result.Configurations, configurationDTO(c.Configuration, c.AssignedCount))
	}
	return connect.NewResponse(result), nil
}

func (h *Handler) DuplicateConfiguration(ctx context.Context, req *connect.Request[pb.DuplicateConfigurationRequest]) (*connect.Response[pb.DuplicateConfigurationResponse], error) {
	c, err := h.service.Duplicate(ctx, req.Msg.AccountId, req.Msg.ConfigurationId, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DuplicateConfigurationResponse{Configuration: configurationDTO(c, 0)}), nil
}

func (h *Handler) ArchiveConfiguration(ctx context.Context, req *connect.Request[pb.ArchiveConfigurationRequest]) (*connect.Response[pb.ArchiveConfigurationResponse], error) {
	if err := h.service.Archive(ctx, req.Msg.AccountId, req.Msg.ConfigurationId, req.Msg.ExpectedVersion); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ArchiveConfigurationResponse{}), nil
}

func (h *Handler) ReviewEdit(ctx context.Context, req *connect.Request[pb.ReviewEditRequest]) (*connect.Response[pb.ReviewEditResponse], error) {
	options, err := inputOptions(req.Msg.Configuration)
	if err != nil {
		return nil, err
	}
	review, err := h.service.ReviewEdit(ctx, req.Msg.AccountId, req.Msg.ConfigurationId, req.Msg.ExpectedVersion, options)
	if err != nil {
		return nil, err
	}
	result := &pb.ReviewEditResponse{ReviewId: review.ID, Configuration: configurationDTO(review.Configuration, review.AssignedCount), SettingsChanged: review.SettingsChanged, ExpiresAt: timestamppb.New(review.ExpiresAt)}
	for _, target := range review.Targets {
		if target.Kind == domain.TargetDevice {
			result.AffectedMiners++
		} else {
			result.AffectedHashboards++
		}
	}
	return connect.NewResponse(result), nil
}

func (h *Handler) ConfirmReview(ctx context.Context, req *connect.Request[pb.ConfirmReviewRequest]) (*connect.Response[pb.ConfirmReviewResponse], error) {
	saved, err := h.service.ConfirmReview(ctx, req.Msg.AccountId, req.Msg.ReviewId)
	if err != nil {
		return nil, err
	}
	result := &pb.ConfirmReviewResponse{Configuration: configurationDTO(saved.Configuration, saved.AssignedCount)}
	if saved.Operation != nil {
		result.OperationId = saved.Operation.ID
	}
	return connect.NewResponse(result), nil
}
