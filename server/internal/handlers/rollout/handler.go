// Package rollout exposes the RolloutService Connect API: firmware release
// channels and the rollouts that enforce them.
package rollout

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/generated/grpc/rollout/v1/rolloutv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// ChannelService is the release channel half of the rollout domain.
type ChannelService interface {
	ListChannels(ctx context.Context, orgID int64) ([]rollout.Channel, error)
	GetChannel(ctx context.Context, orgID, channelID int64) (*rollout.Channel, error)
	ListChannelMiners(ctx context.Context, orgID, channelID int64, manufacturer, model string, pageSize int32, cursor string) ([]rollout.ChannelMiner, string, error)
	ListChannelModelGroups(ctx context.Context, orgID, channelID int64, pageSize int32, cursor string) ([]rollout.ModelGroup, string, error)
	ListMembershipConflicts(ctx context.Context, orgID, channelID int64, pageSize int32, cursor string) ([]rollout.MembershipConflict, string, error)
	CreateChannel(ctx context.Context, orgID, userID int64, spec rollout.ChannelSpec) (*rollout.Channel, error)
	UpdateChannel(ctx context.Context, orgID, channelID int64, spec rollout.ChannelSpec) (*rollout.Channel, error)
	DeleteChannel(ctx context.Context, orgID, channelID int64) error
	PreviewScope(ctx context.Context, orgID int64, scope rollout.Scope, excludeChannelID int64) (*rollout.ScopePreview, error)
}

// AssignmentService is the firmware assignment half of the rollout domain.
type AssignmentService interface {
	PreviewFirmware(ctx context.Context, orgID, channelID int64, assignments []rollout.Assignment, override *rollout.Behavior) ([]rollout.FirmwarePlan, error)
	ApplyFirmware(ctx context.Context, orgID int64, actor rollout.Actor, channelID int64, assignments []rollout.Assignment, override *rollout.Behavior) ([]rollout.Rollout, error)
	RollbackFirmware(ctx context.Context, orgID, rolloutID int64, m rollout.Mutation) (int64, []rollout.Rollout, error)
}

// RolloutService is the rollout lifecycle half of the rollout domain.
type RolloutService interface {
	ListRollouts(ctx context.Context, orgID int64, filter rollout.RolloutFilter) ([]rollout.Rollout, string, string, error)
	GetRollout(ctx context.Context, orgID, rolloutID int64) (*rollout.Rollout, error)
	ListRolloutDevices(ctx context.Context, orgID, rolloutID int64, pageSize int32, cursor string) ([]rollout.RolloutDevice, string, error)
	ContinueRollout(ctx context.Context, orgID, rolloutID int64, m rollout.Mutation) (*rollout.Rollout, error)
	PauseRollout(ctx context.Context, orgID, rolloutID int64, m rollout.Mutation) (*rollout.Rollout, error)
	ResumeRollout(ctx context.Context, orgID, rolloutID int64, m rollout.Mutation) (*rollout.Rollout, error)
	CancelRollout(ctx context.Context, orgID, rolloutID int64, m rollout.Mutation) (*rollout.Rollout, *rollout.Channel, error)
	RetryFailedDevices(ctx context.Context, orgID, rolloutID int64, m rollout.Mutation) (*rollout.Rollout, error)
}

// Service is the slice of the rollout domain the handler uses.
type Service interface {
	ChannelService
	AssignmentService
	RolloutService
}

// Handler serves RolloutService. Delegated control (AdvanceRollout,
// SkipRolloutDevices, CompleteRollout) and the events feed are defined by the
// contract but land in later slices; until then they answer Unimplemented
// through the embedded default handler, and DELEGATED behavior is refused.
type Handler struct {
	rolloutv1connect.UnimplementedRolloutServiceHandler
	svc Service
}

var _ rolloutv1connect.RolloutServiceHandler = &Handler{}

func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// authorize gates every rollout RPC on the firmware-update permission, since
// release channels exist solely to drive firmware updates.
func authorize(ctx context.Context) (*session.Info, error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerFirmwareUpdate, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return session.GetInfo(ctx)
}

// actorOf attributes an action to the caller: an API key or a signed-in user.
func actorOf(info *session.Info) rollout.Actor {
	if info.AuthMethod == session.AuthMethodAPIKey {
		return rollout.Actor{Type: rollout.ActorTypeAPIKey, ID: info.UserID, Name: info.Username}
	}
	return rollout.Actor{Type: rollout.ActorTypeUser, ID: info.UserID, Name: info.Username}
}

func mutation(info *session.Info, expectedRevision int64, note string) rollout.Mutation {
	return rollout.Mutation{Actor: actorOf(info), ExpectedRevision: expectedRevision, Note: note}
}

// withReason attaches the domain's machine-readable cause as a
// RolloutErrorInfo detail so clients can branch on it.
func withReason(err error) error {
	if err == nil {
		return nil
	}
	info, ok := rollout.ReasonOf(err)
	if !ok {
		return err
	}
	var fe fleeterror.FleetError
	if !errors.As(err, &fe) {
		return err
	}
	ce := fe.ConnectError()
	if detail, derr := connect.NewErrorDetail(errorInfoToProto(info)); derr == nil {
		ce.AddDetail(detail)
	}
	return ce
}

// refuseDelegated keeps the DELEGATED method out until the delegated-control
// slice implements it; the proto accepts the value, so the refusal must be
// explicit rather than a silent default.
func refuseDelegated(b *pb.RolloutBehavior) error {
	if b != nil && b.Method == pb.RolloutMethod_ROLLOUT_METHOD_DELEGATED {
		return fleeterror.NewUnimplementedError("delegated rollouts are not available yet")
	}
	return nil
}

func (h *Handler) ListReleaseChannels(ctx context.Context, _ *connect.Request[pb.ListReleaseChannelsRequest]) (*connect.Response[pb.ListReleaseChannelsResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	channels, err := h.svc.ListChannels(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	// Channels are few per org; the list is not paged server-side yet, so
	// the response cursor stays empty.
	resp := &pb.ListReleaseChannelsResponse{}
	for i := range channels {
		resp.Channels = append(resp.Channels, channelSummaryToProto(&channels[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) GetReleaseChannel(ctx context.Context, r *connect.Request[pb.GetReleaseChannelRequest]) (*connect.Response[pb.GetReleaseChannelResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	channel, err := h.svc.GetChannel(ctx, info.OrganizationID, r.Msg.ChannelId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetReleaseChannelResponse{Channel: channelToProto(channel)}), nil
}

func (h *Handler) ListReleaseChannelModelGroups(ctx context.Context, r *connect.Request[pb.ListReleaseChannelModelGroupsRequest]) (*connect.Response[pb.ListReleaseChannelModelGroupsResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	groups, cursor, err := h.svc.ListChannelModelGroups(ctx, info.OrganizationID, r.Msg.ChannelId, r.Msg.PageSize, r.Msg.Cursor)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListReleaseChannelModelGroupsResponse{Cursor: cursor}
	for i := range groups {
		resp.ModelGroups = append(resp.ModelGroups, modelGroupToProto(&groups[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListReleaseChannelMiners(ctx context.Context, r *connect.Request[pb.ListReleaseChannelMinersRequest]) (*connect.Response[pb.ListReleaseChannelMinersResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	miners, cursor, err := h.svc.ListChannelMiners(ctx, info.OrganizationID, r.Msg.ChannelId, r.Msg.Manufacturer, r.Msg.Model, r.Msg.PageSize, r.Msg.Cursor)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListReleaseChannelMinersResponse{Cursor: cursor}
	for i := range miners {
		resp.Miners = append(resp.Miners, minerToProto(&miners[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListReleaseChannelMembershipConflicts(ctx context.Context, r *connect.Request[pb.ListReleaseChannelMembershipConflictsRequest]) (*connect.Response[pb.ListReleaseChannelMembershipConflictsResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	conflicts, cursor, err := h.svc.ListMembershipConflicts(ctx, info.OrganizationID, r.Msg.ChannelId, r.Msg.PageSize, r.Msg.Cursor)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListReleaseChannelMembershipConflictsResponse{Cursor: cursor}
	for i := range conflicts {
		resp.Conflicts = append(resp.Conflicts, conflictToProto(&conflicts[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) CreateReleaseChannel(ctx context.Context, r *connect.Request[pb.CreateReleaseChannelRequest]) (*connect.Response[pb.CreateReleaseChannelResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	if err := refuseDelegated(r.Msg.Behavior); err != nil {
		return nil, err
	}
	channel, err := h.svc.CreateChannel(ctx, info.OrganizationID, info.UserID, rollout.ChannelSpec{
		Name:        r.Msg.Name,
		Description: r.Msg.Description,
		Scope:       scopeFromProto(r.Msg.Scope),
		Behavior:    behaviorFromProto(r.Msg.Behavior),
	})
	if err != nil {
		return nil, withReason(err)
	}
	return connect.NewResponse(&pb.CreateReleaseChannelResponse{Channel: channelToProto(channel)}), nil
}

func (h *Handler) UpdateReleaseChannel(ctx context.Context, r *connect.Request[pb.UpdateReleaseChannelRequest]) (*connect.Response[pb.UpdateReleaseChannelResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	if err := refuseDelegated(r.Msg.Behavior); err != nil {
		return nil, err
	}
	channel, err := h.svc.UpdateChannel(ctx, info.OrganizationID, r.Msg.ChannelId, rollout.ChannelSpec{
		Name:        r.Msg.Name,
		Description: r.Msg.Description,
		Scope:       scopeFromProto(r.Msg.Scope),
		Behavior:    behaviorFromProto(r.Msg.Behavior),
	})
	if err != nil {
		return nil, withReason(err)
	}
	return connect.NewResponse(&pb.UpdateReleaseChannelResponse{Channel: channelToProto(channel)}), nil
}

func (h *Handler) DeleteReleaseChannel(ctx context.Context, r *connect.Request[pb.DeleteReleaseChannelRequest]) (*connect.Response[pb.DeleteReleaseChannelResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteChannel(ctx, info.OrganizationID, r.Msg.ChannelId); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteReleaseChannelResponse{}), nil
}

func (h *Handler) PreviewReleaseChannelScope(ctx context.Context, r *connect.Request[pb.PreviewReleaseChannelScopeRequest]) (*connect.Response[pb.PreviewReleaseChannelScopeResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	preview, err := h.svc.PreviewScope(ctx, info.OrganizationID, scopeFromProto(r.Msg.Scope), r.Msg.ChannelId)
	if err != nil {
		return nil, err
	}
	resp := &pb.PreviewReleaseChannelScopeResponse{
		MinerCount:    preview.MinerCount,
		ModelCount:    preview.ModelCount,
		ConflictCount: preview.ConflictCount,
	}
	for _, m := range preview.Models {
		resp.Models = append(resp.Models, &pb.ReleaseChannelScopeModelCount{Manufacturer: m.Manufacturer, Model: m.Model, MinerCount: m.MinerCount})
	}
	for _, c := range preview.Conflicts {
		resp.Conflicts = append(resp.Conflicts, &pb.ReleaseChannelScopeConflict{ChannelId: c.ChannelID, ChannelName: c.ChannelName, MinerCount: c.MinerCount})
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) PreviewReleaseChannelFirmware(ctx context.Context, r *connect.Request[pb.PreviewReleaseChannelFirmwareRequest]) (*connect.Response[pb.PreviewReleaseChannelFirmwareResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	if err := refuseDelegated(r.Msg.BehaviorOverride); err != nil {
		return nil, err
	}
	plans, err := h.svc.PreviewFirmware(ctx, info.OrganizationID, r.Msg.ChannelId, assignmentsFromProto(r.Msg.Assignments), overrideFromProto(r.Msg.BehaviorOverride))
	if err != nil {
		return nil, withReason(err)
	}
	resp := &pb.PreviewReleaseChannelFirmwareResponse{}
	for i := range plans {
		resp.Plans = append(resp.Plans, planToProto(&plans[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ApplyReleaseChannelFirmware(ctx context.Context, r *connect.Request[pb.ApplyReleaseChannelFirmwareRequest]) (*connect.Response[pb.ApplyReleaseChannelFirmwareResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	if err := refuseDelegated(r.Msg.BehaviorOverride); err != nil {
		return nil, err
	}
	started, err := h.svc.ApplyFirmware(ctx, info.OrganizationID, actorOf(info), r.Msg.ChannelId, assignmentsFromProto(r.Msg.Assignments), overrideFromProto(r.Msg.BehaviorOverride))
	if err != nil {
		return nil, withReason(err)
	}
	channel, err := h.svc.GetChannel(ctx, info.OrganizationID, r.Msg.ChannelId)
	if err != nil {
		return nil, err
	}
	resp := &pb.ApplyReleaseChannelFirmwareResponse{Channel: channelToProto(channel)}
	for i := range started {
		resp.StartedRollouts = append(resp.StartedRollouts, rolloutToProto(&started[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) RollbackReleaseChannelFirmware(ctx context.Context, r *connect.Request[pb.RollbackReleaseChannelFirmwareRequest]) (*connect.Response[pb.RollbackReleaseChannelFirmwareResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	channelID, started, err := h.svc.RollbackFirmware(ctx, info.OrganizationID, r.Msg.RolloutId, mutation(info, r.Msg.ExpectedRevision, r.Msg.Note))
	if err != nil {
		return nil, withReason(err)
	}
	channel, err := h.svc.GetChannel(ctx, info.OrganizationID, channelID)
	if err != nil {
		return nil, err
	}
	resp := &pb.RollbackReleaseChannelFirmwareResponse{Channel: channelToProto(channel)}
	for i := range started {
		resp.StartedRollouts = append(resp.StartedRollouts, rolloutToProto(&started[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListRollouts(ctx context.Context, r *connect.Request[pb.ListRolloutsRequest]) (*connect.Response[pb.ListRolloutsResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	filter := rollout.RolloutFilter{
		ChannelID:  r.Msg.ChannelId,
		PageSize:   r.Msg.PageSize,
		Cursor:     r.Msg.Cursor,
		PollCursor: r.Msg.PollCursor,
	}
	if r.Msg.Status != pb.RolloutStatus_ROLLOUT_STATUS_UNSPECIFIED {
		status, ok := statusFromProto[r.Msg.Status]
		if !ok {
			return nil, fleeterror.NewInvalidArgumentErrorf("unknown rollout status %q", r.Msg.Status.String())
		}
		filter.Status = status
	}
	if r.Msg.UpdatedAfter != nil {
		after := r.Msg.UpdatedAfter.AsTime()
		filter.UpdatedAfter = &after
	}
	rollouts, cursor, pollCursor, err := h.svc.ListRollouts(ctx, info.OrganizationID, filter)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListRolloutsResponse{Cursor: cursor, PollCursor: pollCursor}
	for i := range rollouts {
		resp.Rollouts = append(resp.Rollouts, rolloutToProto(&rollouts[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) GetRollout(ctx context.Context, r *connect.Request[pb.GetRolloutRequest]) (*connect.Response[pb.GetRolloutResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.svc.GetRollout(ctx, info.OrganizationID, r.Msg.RolloutId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetRolloutResponse{Rollout: rolloutToProto(view)}), nil
}

func (h *Handler) ListRolloutDevices(ctx context.Context, r *connect.Request[pb.ListRolloutDevicesRequest]) (*connect.Response[pb.ListRolloutDevicesResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	devices, cursor, err := h.svc.ListRolloutDevices(ctx, info.OrganizationID, r.Msg.RolloutId, r.Msg.PageSize, r.Msg.Cursor)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListRolloutDevicesResponse{Cursor: cursor}
	for i := range devices {
		resp.Devices = append(resp.Devices, deviceToProto(&devices[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ContinueRollout(ctx context.Context, r *connect.Request[pb.ContinueRolloutRequest]) (*connect.Response[pb.ContinueRolloutResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.svc.ContinueRollout(ctx, info.OrganizationID, r.Msg.RolloutId, mutation(info, r.Msg.ExpectedRevision, r.Msg.Note))
	if err != nil {
		return nil, withReason(err)
	}
	return connect.NewResponse(&pb.ContinueRolloutResponse{Rollout: rolloutToProto(view)}), nil
}

func (h *Handler) PauseRollout(ctx context.Context, r *connect.Request[pb.PauseRolloutRequest]) (*connect.Response[pb.PauseRolloutResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.svc.PauseRollout(ctx, info.OrganizationID, r.Msg.RolloutId, mutation(info, r.Msg.ExpectedRevision, ""))
	if err != nil {
		return nil, withReason(err)
	}
	return connect.NewResponse(&pb.PauseRolloutResponse{Rollout: rolloutToProto(view)}), nil
}

func (h *Handler) ResumeRollout(ctx context.Context, r *connect.Request[pb.ResumeRolloutRequest]) (*connect.Response[pb.ResumeRolloutResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.svc.ResumeRollout(ctx, info.OrganizationID, r.Msg.RolloutId, mutation(info, r.Msg.ExpectedRevision, ""))
	if err != nil {
		return nil, withReason(err)
	}
	return connect.NewResponse(&pb.ResumeRolloutResponse{Rollout: rolloutToProto(view)}), nil
}

func (h *Handler) CancelRollout(ctx context.Context, r *connect.Request[pb.CancelRolloutRequest]) (*connect.Response[pb.CancelRolloutResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	view, channel, err := h.svc.CancelRollout(ctx, info.OrganizationID, r.Msg.RolloutId, mutation(info, r.Msg.ExpectedRevision, r.Msg.Note))
	if err != nil {
		return nil, withReason(err)
	}
	return connect.NewResponse(&pb.CancelRolloutResponse{Rollout: rolloutToProto(view), Channel: channelToProto(channel)}), nil
}

func (h *Handler) RetryFailedRolloutDevices(ctx context.Context, r *connect.Request[pb.RetryFailedRolloutDevicesRequest]) (*connect.Response[pb.RetryFailedRolloutDevicesResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.svc.RetryFailedDevices(ctx, info.OrganizationID, r.Msg.RolloutId, mutation(info, r.Msg.ExpectedRevision, ""))
	if err != nil {
		return nil, withReason(err)
	}
	return connect.NewResponse(&pb.RetryFailedRolloutDevicesResponse{Rollout: rolloutToProto(view)}), nil
}
