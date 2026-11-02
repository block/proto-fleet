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
	return connect.NewResponse(reviewDTO(review)), nil
}

func reviewDTO(review domain.Review) *pb.ReviewEditResponse {
	result := &pb.ReviewEditResponse{ReviewId: review.ID, Configuration: configurationDTO(review.Configuration, review.AssignedCount), SettingsChanged: review.SettingsChanged, ExpiresAt: timestamppb.New(review.ExpiresAt)}
	for _, target := range review.Targets {
		if target.Kind == domain.TargetDevice {
			result.AffectedMiners++
		} else {
			result.AffectedHashboards++
		}
	}
	return result
}

func (h *Handler) ConfirmReview(ctx context.Context, req *connect.Request[pb.ConfirmReviewRequest]) (*connect.Response[pb.ConfirmReviewResponse], error) {
	saved, err := h.service.ConfirmReview(ctx, req.Msg.AccountId, req.Msg.ReviewId, req.Msg.Password)
	if err != nil {
		return nil, err
	}
	result := &pb.ConfirmReviewResponse{Configuration: configurationDTO(saved.Configuration, saved.AssignedCount)}
	if saved.Operation != nil {
		result.OperationId = saved.Operation.ID
	}
	return connect.NewResponse(result), nil
}

func (h *Handler) ReviewAssignment(ctx context.Context, req *connect.Request[pb.ReviewAssignmentRequest]) (*connect.Response[pb.ReviewAssignmentResponse], error) {
	review, err := h.service.ReviewDevices(ctx, req.Msg.AccountId, req.Msg.ConfigurationId, req.Msg.ExpectedVersion, req.Msg.DeviceIdentifiers)
	if err != nil {
		return nil, err
	}
	result := reviewDTO(review)
	return connect.NewResponse(&pb.ReviewAssignmentResponse{ReviewId: result.ReviewId, Configuration: result.Configuration, AffectedMiners: result.AffectedMiners, AffectedHashboards: result.AffectedHashboards, ExpiresAt: result.ExpiresAt}), nil
}
func (h *Handler) GetOperation(ctx context.Context, req *connect.Request[pb.GetOperationRequest]) (*connect.Response[pb.GetOperationResponse], error) {
	operation, err := h.service.GetOperation(ctx, req.Msg.AccountId, req.Msg.OperationId)
	if err != nil {
		return nil, err
	}
	result := &pb.GetOperationResponse{OperationId: operation.ID, ConfigurationId: operation.ConfigurationID}
	for _, target := range operation.Targets {
		result.Targets = append(result.Targets, &pb.OperationTarget{Kind: target.Kind, TargetId: target.ID, State: target.State, ResultCode: target.ResultCode, Label: target.Label})
	}
	return connect.NewResponse(result), nil
}

func (h *Handler) ReviewRetry(ctx context.Context, req *connect.Request[pb.ReviewRetryRequest]) (*connect.Response[pb.ReviewRetryResponse], error) {
	targets := make([]domain.AssignmentTarget, len(req.Msg.Targets))
	for i, target := range req.Msg.Targets {
		if target == nil {
			return nil, fleeterror.NewInvalidArgumentError("retry target is required")
		}
		targets[i] = domain.AssignmentTarget{Kind: target.Kind, ID: target.TargetId}
	}
	review, err := h.service.ReviewRetry(ctx, req.Msg.AccountId, req.Msg.OperationId, targets)
	if err != nil {
		return nil, err
	}
	result := reviewDTO(review)
	return connect.NewResponse(&pb.ReviewRetryResponse{ReviewId: result.ReviewId, Configuration: result.Configuration, AffectedMiners: result.AffectedMiners, AffectedHashboards: result.AffectedHashboards, ExpiresAt: result.ExpiresAt}), nil
}

func (h *Handler) ListOperations(ctx context.Context, req *connect.Request[pb.ListOperationsRequest]) (*connect.Response[pb.ListOperationsResponse], error) {
	page, err := h.service.ListOperations(ctx, req.Msg.AccountId, req.Msg.ConfigurationId, req.Msg.PageToken)
	if err != nil {
		return nil, err
	}
	result := &pb.ListOperationsResponse{NextPageToken: page.NextPageToken}
	for _, operation := range page.Operations {
		result.Operations = append(result.Operations, &pb.OperationSummary{OperationId: operation.OperationID, CreatedAt: timestamppb.New(operation.CreatedAt), TargetCount: operation.TargetCount, PendingCount: operation.PendingCount, FailedCount: operation.FailedCount})
	}
	return connect.NewResponse(result), nil
}
