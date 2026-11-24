package poolconfigurations

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/fleetmanagement"

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

func (h *Handler) GetAssignments(ctx context.Context, req *connect.Request[pb.GetAssignmentsRequest]) (*connect.Response[pb.GetAssignmentsResponse], error) {
	if boards := req.Msg.Boards; boards != nil {
		result, err := h.service.GetBoardAssignments(ctx, req.Msg.AccountId, domain.BoardSelection{ModuleIDs: boards.ModuleIds, HashboardIDs: boards.HashboardIds, DeviceIDs: req.Msg.DeviceIds})
		if err != nil {
			return nil, err
		}
		response := &pb.GetAssignmentsResponse{SelectedCount: result.SelectedCount}
		for _, row := range result.Assignments {
			response.Assignments = append(response.Assignments, &pb.EquipmentAssignment{DeviceId: row.DeviceID, DeviceIdentifier: row.DeviceIdentifier, ConfigurationId: row.ConfigurationID, Name: row.Name, OperationId: row.OperationID, State: row.State, ResultCode: row.ResultCode, InstallationId: row.InstallationID, HashboardId: row.HashboardID, ModuleId: row.ModuleID, UpdatedAt: timestamppb.New(row.UpdatedAt)})
		}
		if len(req.Msg.DeviceIds) > 0 {
			miners, err := h.service.GetAssignments(ctx, req.Msg.AccountId, nil, req.Msg.DeviceIds, nil)
			if err != nil {
				return nil, err
			}
			response.SelectedCount += miners.SelectedCount
			for _, row := range miners.Assignments {
				response.Assignments = append(response.Assignments, &pb.EquipmentAssignment{DeviceId: row.DeviceID, DeviceIdentifier: row.DeviceIdentifier, DeviceName: fleetmanagement.ComposeDeviceName(row.CustomName, row.Manufacturer, row.Model), MacAddress: row.MacAddress, ConfigurationId: row.ConfigurationID, Name: row.Name, OperationId: row.OperationID, State: row.State, ResultCode: row.ResultCode, UpdatedAt: timestamppb.New(row.UpdatedAt)})
			}
		}
		return connect.NewResponse(response), nil
	}
	result, err := h.service.GetAssignments(ctx, req.Msg.AccountId, req.Msg.DeviceIdentifiers, req.Msg.DeviceIds, req.Msg.Filter)
	if err != nil {
		return nil, err
	}
	response := &pb.GetAssignmentsResponse{SelectedCount: result.SelectedCount}
	for _, row := range result.Assignments {
		response.Assignments = append(response.Assignments, &pb.EquipmentAssignment{DeviceId: row.DeviceID, DeviceIdentifier: row.DeviceIdentifier, DeviceName: fleetmanagement.ComposeDeviceName(row.CustomName, row.Manufacturer, row.Model), MacAddress: row.MacAddress, ConfigurationId: row.ConfigurationID, Name: row.Name, OperationId: row.OperationID, State: row.State, ResultCode: row.ResultCode, UpdatedAt: timestamppb.New(row.UpdatedAt)})
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) TestConnection(ctx context.Context, req *connect.Request[pb.TestConnectionRequest]) (*connect.Response[pb.TestConnectionResponse], error) {
	pool := req.Msg.Pool
	if pool == nil {
		return nil, fleeterror.NewInvalidArgumentError("pool is required")
	}
	connected, err := h.service.TestConnection(ctx, req.Msg.AccountId, req.Msg.ConfigurationId, req.Msg.ExpectedVersion, domain.SlotOptions{ID: pool.SlotId, URL: pool.Url, Username: pool.Username, Password: pool.Password, WorkerTemplate: pool.WorkerTemplate})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.TestConnectionResponse{Connected: connected}), nil
}

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
	result := &pb.Configuration{ConfigurationId: c.ID, Name: c.Name, RevisionId: c.RevisionID, Version: c.Version, Archived: c.Archived, IsDefault: c.IsDefault, AssignedCount: assignedCount}
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
	for _, equipment := range review.Equipment {
		result.Equipment = append(result.Equipment, &pb.ReviewedEquipment{Kind: equipment.Kind, TargetId: equipment.ID, Label: equipment.Label, DeviceIdentifier: equipment.DeviceIdentifier})
	}
	return result
}

func (h *Handler) AuthorizeWorkerNameReviews(ctx context.Context, req *connect.Request[pb.AuthorizeWorkerNameReviewsRequest]) (*connect.Response[pb.AuthorizeWorkerNameReviewsResponse], error) {
	if err := h.service.AuthorizeWorkerNameReviews(ctx, req.Msg.ReviewIds, req.Msg.Password); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AuthorizeWorkerNameReviewsResponse{}), nil
}

func (h *Handler) AuthorizeReviews(ctx context.Context, req *connect.Request[pb.AuthorizeReviewsRequest]) (*connect.Response[pb.AuthorizeReviewsResponse], error) {
	if err := h.service.AuthorizeReviews(ctx, req.Msg.AccountId, req.Msg.ReviewIds, req.Msg.Password); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AuthorizeReviewsResponse{}), nil
}

func (h *Handler) ConfirmReview(ctx context.Context, req *connect.Request[pb.ConfirmReviewRequest]) (*connect.Response[pb.ConfirmReviewResponse], error) {
	saved, err := h.service.ConfirmReview(ctx, req.Msg.AccountId, req.Msg.ReviewId, domain.Confirmation{Password: req.Msg.Password, KeepDeviceSettings: req.Msg.KeepDeviceSettings})
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
	if target := req.Msg.RemovalTarget; target != nil {
		r, err := h.service.ReviewRemoval(ctx, req.Msg.AccountId, domain.AssignmentTarget{Kind: target.Kind, ID: target.TargetId})
		if err != nil {
			return nil, err
		}
		operationID := ""
		if r.State != "reviewed" {
			operationID = r.ID
		}
		return connect.NewResponse(&pb.ReviewAssignmentResponse{ReviewId: r.ID, Removal: &pb.RemovalPreflight{VerificationState: r.VerificationState, ClearSupported: r.ClearSupported, OperationId: operationID, KeepDeviceSettings: r.KeepDeviceSettings}}), nil
	}
	var review domain.Review
	var err error
	if boards := req.Msg.Boards; boards != nil {
		review, err = h.service.ReviewBoards(ctx, req.Msg.AccountId, req.Msg.ConfigurationId, req.Msg.ExpectedVersion, domain.BoardSelection{ModuleIDs: boards.ModuleIds, HashboardIDs: boards.HashboardIds, DeviceIDs: req.Msg.DeviceIds})
	} else {
		review, err = h.service.ReviewSelection(ctx, req.Msg.AccountId, req.Msg.ConfigurationId, req.Msg.ExpectedVersion, req.Msg.DeviceIdentifiers, req.Msg.DeviceIds, req.Msg.Filter)
	}
	if err != nil {
		return nil, err
	}
	result := reviewDTO(review)
	return connect.NewResponse(&pb.ReviewAssignmentResponse{ReviewId: result.ReviewId, Configuration: result.Configuration, AffectedMiners: result.AffectedMiners, AffectedHashboards: result.AffectedHashboards, ExpiresAt: result.ExpiresAt, Equipment: result.Equipment}), nil
}
func (h *Handler) GetOperation(ctx context.Context, req *connect.Request[pb.GetOperationRequest]) (*connect.Response[pb.GetOperationResponse], error) {
	operation, err := h.service.GetOperation(ctx, req.Msg.AccountId, req.Msg.OperationId)
	if err != nil {
		return nil, err
	}
	result := &pb.GetOperationResponse{Kind: operation.Kind, OperationId: operation.ID, ConfigurationId: operation.ConfigurationID, ConfigurationName: operation.Name, CreatedAt: timestamppb.New(operation.CreatedAt)}
	for _, target := range operation.Targets {
		result.Targets = append(result.Targets, &pb.OperationTarget{Kind: target.Kind, TargetId: target.ID, State: target.State, ResultCode: target.ResultCode, Label: target.Label, MacAddress: target.MACAddress, DeviceIdentifier: target.DeviceIdentifier, UpdatedAt: timestamppb.New(target.UpdatedAt)})
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
	return connect.NewResponse(&pb.ReviewRetryResponse{ReviewId: result.ReviewId, Configuration: result.Configuration, AffectedMiners: result.AffectedMiners, AffectedHashboards: result.AffectedHashboards, ExpiresAt: result.ExpiresAt, Equipment: result.Equipment}), nil
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

func (h *Handler) CheckConfiguration(ctx context.Context, req *connect.Request[pb.CheckConfigurationRequest]) (*connect.Response[pb.CheckConfigurationResponse], error) {
	if req.Msg.Target == nil {
		return nil, fleeterror.NewInvalidArgumentError("select equipment")
	}
	result, err := h.service.CheckConfiguration(ctx, req.Msg.AccountId, req.Msg.ConfigurationId, domain.AssignmentTarget{Kind: req.Msg.Target.Kind, ID: req.Msg.Target.TargetId})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(checkResponse(result)), nil
}

func (h *Handler) ReadDevicePools(ctx context.Context, req *connect.Request[pb.ReadDevicePoolsRequest]) (*connect.Response[pb.ReadDevicePoolsResponse], error) {
	targets := make([]domain.AssignmentTarget, len(req.Msg.Targets))
	for i, target := range req.Msg.Targets {
		if target == nil {
			return nil, fleeterror.NewInvalidArgumentError("select equipment")
		}
		targets[i] = domain.AssignmentTarget{Kind: target.Kind, ID: target.TargetId}
	}
	results, err := h.service.ReadDevicePools(ctx, req.Msg.AccountId, targets)
	if err != nil {
		return nil, err
	}
	response := &pb.ReadDevicePoolsResponse{}
	for i, result := range results {
		response.Readings = append(response.Readings, &pb.DevicePoolReading{Target: req.Msg.Targets[i], Reading: checkResponse(result)})
	}
	return connect.NewResponse(response), nil
}

func checkResponse(result domain.CheckResult) *pb.CheckConfigurationResponse {
	response := &pb.CheckConfigurationResponse{State: result.State, ResultCode: result.ResultCode, ReadbackAvailable: result.ReadbackAvailable}
	if !result.CheckedAt.IsZero() {
		response.CheckedAt = timestamppb.New(result.CheckedAt)
	}
	for _, p := range result.Expected {
		response.Expected = append(response.Expected, &pb.ReportedPool{Priority: uint32(p.Priority), Url: p.URL, Username: p.Username})
	} // #nosec G115 -- bounded pool priorities.
	for _, p := range result.Reported {
		if p.Priority >= 0 {
			response.Reported = append(response.Reported, &pb.ReportedPool{Priority: uint32(p.Priority), Url: p.URL, Username: p.Username})
		}
	} // #nosec G115 -- nonnegative int32 priority.
	return response
}

func (h *Handler) SetDefaultConfiguration(ctx context.Context, req *connect.Request[pb.SetDefaultConfigurationRequest]) (*connect.Response[pb.SetDefaultConfigurationResponse], error) {
	if err := h.service.SetDefault(ctx, req.Msg.AccountId, req.Msg.ConfigurationId, req.Msg.ExpectedVersion); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetDefaultConfigurationResponse{}), nil
}
