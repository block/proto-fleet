package curtailment

import (
	"context"
	"math"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func (h *Handler) ListMqttCurtailmentSources(ctx context.Context, _ *connect.Request[pb.ListMqttCurtailmentSourcesRequest]) (*connect.Response[pb.ListMqttCurtailmentSourcesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.mqttSettings == nil {
		return nil, errCurtailmentNotImplemented("ListMqttCurtailmentSources")
	}
	views, err := h.mqttSettings.List(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	out := make([]*pb.MqttCurtailmentSource, len(views))
	for i, view := range views {
		out[i] = toMqttSourceProto(view)
	}
	return connect.NewResponse(&pb.ListMqttCurtailmentSourcesResponse{Sources: out}), nil
}

func (h *Handler) GetMqttCurtailmentSource(ctx context.Context, req *connect.Request[pb.GetMqttCurtailmentSourceRequest]) (*connect.Response[pb.GetMqttCurtailmentSourceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.mqttSettings == nil {
		return nil, errCurtailmentNotImplemented("GetMqttCurtailmentSource")
	}
	view, err := h.mqttSettings.Get(ctx, info.OrganizationID, req.Msg.GetSourceId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetMqttCurtailmentSourceResponse{Source: toMqttSourceProto(view)}), nil
}

func (h *Handler) CreateMqttCurtailmentSource(ctx context.Context, req *connect.Request[pb.CreateMqttCurtailmentSourceRequest]) (*connect.Response[pb.CreateMqttCurtailmentSourceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.mqttSettings == nil {
		return nil, errCurtailmentNotImplemented("CreateMqttCurtailmentSource")
	}
	source := mqttingest.SourceConfig{
		OrganizationID:          info.OrganizationID,
		ServiceUserID:           req.Msg.GetServiceUserId(),
		SourceName:              req.Msg.GetSourceName(),
		Topic:                   req.Msg.GetTopic(),
		BrokerPrimaryHost:       req.Msg.GetBrokerPrimaryHost(),
		BrokerSecondaryHost:     req.Msg.GetBrokerSecondaryHost(),
		BrokerPort:              req.Msg.GetBrokerPort(),
		BrokerTransport:         req.Msg.GetBrokerTransport(),
		MQTTUsername:            req.Msg.GetMqttUsername(),
		ContractedCurtailmentKw: req.Msg.GetContractedCurtailmentKw(),
		CurtailMode:             req.Msg.GetCurtailMode(),
		PayloadFormat:           req.Msg.GetPayloadFormat(),
		StalenessThreshold:      durationFromSeconds(req.Msg.GetStalenessThresholdSec()),
		MinCurtailedDuration:    durationFromSeconds(req.Msg.GetMinCurtailedDurationSec()),
		Enabled:                 req.Msg.GetEnabled(),
	}
	if scope := req.Msg.GetScope(); scope != nil {
		applyMqttScope(scope, &source)
	}
	view, err := h.mqttSettings.Create(ctx, mqttingest.CreateSourceRequest{
		Source:            source,
		PlaintextPassword: req.Msg.GetMqttPassword(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateMqttCurtailmentSourceResponse{Source: toMqttSourceProto(view)}), nil
}

func (h *Handler) UpdateMqttCurtailmentSource(ctx context.Context, req *connect.Request[pb.UpdateMqttCurtailmentSourceRequest]) (*connect.Response[pb.UpdateMqttCurtailmentSourceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.mqttSettings == nil {
		return nil, errCurtailmentNotImplemented("UpdateMqttCurtailmentSource")
	}
	if err := validateMqttUpdateClears(req.Msg); err != nil {
		return nil, err
	}
	updateReq := mqttingest.UpdateSourceRequest{
		OrganizationID:      info.OrganizationID,
		SourceID:            req.Msg.GetSourceId(),
		SourceName:          req.Msg.SourceName,
		Topic:               req.Msg.Topic,
		BrokerPrimaryHost:   req.Msg.BrokerPrimaryHost,
		BrokerSecondaryHost: req.Msg.BrokerSecondaryHost,
		BrokerPort:          req.Msg.BrokerPort,
		BrokerTransport:     req.Msg.BrokerTransport,
		MQTTUsername:        req.Msg.MqttUsername,
		PlaintextPassword:   req.Msg.MqttPassword,
		CurtailMode:         req.Msg.CurtailMode,
		ContractedKw:        req.Msg.ContractedCurtailmentKw,
		ClearContractedKw:   req.Msg.GetClearContractedCurtailmentKw(),
		PayloadFormat:       req.Msg.PayloadFormat,
		ClearStaleness:      req.Msg.GetClearStalenessThresholdSec(),
		ClearMinCurtailed:   req.Msg.GetClearMinCurtailedDurationSec(),
		ServiceUserID:       req.Msg.ServiceUserId,
	}
	if req.Msg.Scope != nil {
		scope := toMqttDomainScope(req.Msg.Scope)
		updateReq.Scope = &scope
	}
	if req.Msg.StalenessThresholdSec != nil {
		d := durationFromSeconds(req.Msg.GetStalenessThresholdSec())
		updateReq.StalenessThreshold = &d
	}
	if req.Msg.MinCurtailedDurationSec != nil {
		d := durationFromSeconds(req.Msg.GetMinCurtailedDurationSec())
		updateReq.MinCurtailed = &d
	}
	view, err := h.mqttSettings.Update(ctx, updateReq)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateMqttCurtailmentSourceResponse{Source: toMqttSourceProto(view)}), nil
}

func (h *Handler) SetMqttCurtailmentSourceEnabled(ctx context.Context, req *connect.Request[pb.SetMqttCurtailmentSourceEnabledRequest]) (*connect.Response[pb.SetMqttCurtailmentSourceEnabledResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.mqttSettings == nil {
		return nil, errCurtailmentNotImplemented("SetMqttCurtailmentSourceEnabled")
	}
	view, err := h.mqttSettings.SetEnabled(ctx, info.OrganizationID, req.Msg.GetSourceId(), req.Msg.GetEnabled())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetMqttCurtailmentSourceEnabledResponse{Source: toMqttSourceProto(view)}), nil
}

func (h *Handler) DeleteMqttCurtailmentSource(ctx context.Context, req *connect.Request[pb.DeleteMqttCurtailmentSourceRequest]) (*connect.Response[pb.DeleteMqttCurtailmentSourceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.mqttSettings == nil {
		return nil, errCurtailmentNotImplemented("DeleteMqttCurtailmentSource")
	}
	if err := h.mqttSettings.Delete(ctx, info.OrganizationID, req.Msg.GetSourceId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteMqttCurtailmentSourceResponse{}), nil
}

func applyMqttScope(scope *pb.MqttCurtailmentSourceScope, target *mqttingest.SourceConfig) {
	domainScope := toMqttDomainScope(scope)
	target.ScopeType = domainScope.Type
	target.ScopeSiteID = domainScope.SiteID
	target.ScopeDeviceIdentifiers = domainScope.DeviceIdentifiers
}

func toMqttDomainScope(scope *pb.MqttCurtailmentSourceScope) mqttingest.SourceScope {
	out := mqttingest.SourceScope{
		SiteID:            scope.SiteId,
		DeviceIdentifiers: scope.GetDeviceIdentifiers(),
	}
	switch scope.GetType() {
	case pb.MqttCurtailmentSourceScopeType_MQTT_CURTAILMENT_SOURCE_SCOPE_TYPE_UNSPECIFIED:
		out.Type = ""
	case pb.MqttCurtailmentSourceScopeType_MQTT_CURTAILMENT_SOURCE_SCOPE_TYPE_WHOLE_ORG:
		out.Type = string(models.ScopeTypeWholeOrg)
	case pb.MqttCurtailmentSourceScopeType_MQTT_CURTAILMENT_SOURCE_SCOPE_TYPE_SITE:
		out.Type = "site"
	case pb.MqttCurtailmentSourceScopeType_MQTT_CURTAILMENT_SOURCE_SCOPE_TYPE_DEVICE_LIST:
		out.Type = string(models.ScopeTypeDeviceList)
	default:
		out.Type = scope.GetType().String()
	}
	return out
}

func toMqttSourceProto(view mqttingest.SourceView) *pb.MqttCurtailmentSource {
	cfg := view.Config
	out := &pb.MqttCurtailmentSource{
		SourceId:                cfg.ID,
		SourceName:              cfg.SourceName,
		Topic:                   cfg.Topic,
		BrokerPrimaryHost:       cfg.BrokerPrimaryHost,
		BrokerSecondaryHost:     cfg.BrokerSecondaryHost,
		BrokerPort:              cfg.BrokerPort,
		BrokerTransport:         cfg.BrokerTransport,
		MqttUsername:            cfg.MQTTUsername,
		HasPassword:             cfg.MQTTPasswordEncrypted != "",
		CurtailMode:             cfg.CurtailMode,
		PayloadFormat:           cfg.PayloadFormat,
		Scope:                   toMqttScopeProto(cfg),
		StalenessThresholdSec:   durationSecondsToUint32(cfg.StalenessThreshold),
		MinCurtailedDurationSec: durationSecondsToUint32(cfg.MinCurtailedDuration),
		Enabled:                 cfg.Enabled,
		ServiceUserId:           cfg.ServiceUserID,
		CreatedAt:               mqttTimeProto(cfg.CreatedAt),
		UpdatedAt:               mqttTimeProto(cfg.UpdatedAt),
		Status:                  toMqttStatusProto(view),
	}
	if cfg.ContractedCurtailmentKw > 0 {
		out.ContractedCurtailmentKw = &cfg.ContractedCurtailmentKw
	}
	return out
}

func toMqttScopeProto(cfg mqttingest.SourceConfig) *pb.MqttCurtailmentSourceScope {
	out := &pb.MqttCurtailmentSourceScope{
		SiteId:            cfg.ScopeSiteID,
		DeviceIdentifiers: append([]string(nil), cfg.ScopeDeviceIdentifiers...),
	}
	switch cfg.ScopeType {
	case "", string(models.ScopeTypeWholeOrg):
		out.Type = pb.MqttCurtailmentSourceScopeType_MQTT_CURTAILMENT_SOURCE_SCOPE_TYPE_WHOLE_ORG
	case "site":
		out.Type = pb.MqttCurtailmentSourceScopeType_MQTT_CURTAILMENT_SOURCE_SCOPE_TYPE_SITE
	case string(models.ScopeTypeDeviceList):
		out.Type = pb.MqttCurtailmentSourceScopeType_MQTT_CURTAILMENT_SOURCE_SCOPE_TYPE_DEVICE_LIST
	default:
		out.Type = pb.MqttCurtailmentSourceScopeType_MQTT_CURTAILMENT_SOURCE_SCOPE_TYPE_UNSPECIFIED
	}
	return out
}

func toMqttStatusProto(view mqttingest.SourceView) *pb.MqttCurtailmentSourceStatus {
	out := &pb.MqttCurtailmentSourceStatus{
		RuntimeState:          toMqttRuntimeStateProto(view.Runtime.State),
		LastRuntimeError:      view.Runtime.LastError,
		RunningBrokerCount:    intToInt32Saturating(view.Runtime.RunningBrokerCount),
		SubscribedBrokerCount: intToInt32Saturating(view.Runtime.SubscribedBrokerCount),
		Stale:                 view.Stale,
	}
	if !view.HasState {
		return out
	}
	state := view.State
	out.LastTarget = state.LastTarget.String()
	out.LastTargetAt = mqttTimeProto(state.LastTargetAt)
	out.LastReceivedAt = mqttTimeProto(state.LastReceivedAt)
	out.LastReceivedBroker = state.LastReceivedBroker
	out.LastEdgeAt = mqttTimeProto(state.LastEdgeAt)
	out.LastEdgeEventUuid = state.LastEdgeEventUUID
	if state.PendingEdge != nil {
		out.PendingDirection = state.PendingEdge.Direction.String()
		out.PendingTarget = state.PendingEdge.Target.String()
		out.PendingTargetAt = mqttTimeProto(state.PendingEdge.TargetAt)
		out.PendingReceivedAt = mqttTimeProto(state.PendingEdge.ReceivedAt)
		out.PendingReceivedBroker = state.PendingEdge.ReceivedBroker
		out.PendingPriorEdgeAt = mqttTimeProto(state.PendingEdge.PriorEdgeAt)
		out.PendingRetryAt = mqttTimeProto(state.PendingEdge.RetryAt)
	}
	return out
}

func toMqttRuntimeStateProto(state mqttingest.RuntimeState) pb.MqttCurtailmentSourceRuntimeState {
	switch state {
	case mqttingest.RuntimeStateUnspecified:
		return pb.MqttCurtailmentSourceRuntimeState_MQTT_CURTAILMENT_SOURCE_RUNTIME_STATE_UNSPECIFIED
	case mqttingest.RuntimeStateDisabled:
		return pb.MqttCurtailmentSourceRuntimeState_MQTT_CURTAILMENT_SOURCE_RUNTIME_STATE_DISABLED
	case mqttingest.RuntimeStateStopped:
		return pb.MqttCurtailmentSourceRuntimeState_MQTT_CURTAILMENT_SOURCE_RUNTIME_STATE_STOPPED
	case mqttingest.RuntimeStateStarting:
		return pb.MqttCurtailmentSourceRuntimeState_MQTT_CURTAILMENT_SOURCE_RUNTIME_STATE_STARTING
	case mqttingest.RuntimeStateRunning:
		return pb.MqttCurtailmentSourceRuntimeState_MQTT_CURTAILMENT_SOURCE_RUNTIME_STATE_RUNNING
	case mqttingest.RuntimeStateError:
		return pb.MqttCurtailmentSourceRuntimeState_MQTT_CURTAILMENT_SOURCE_RUNTIME_STATE_ERROR
	default:
		return pb.MqttCurtailmentSourceRuntimeState_MQTT_CURTAILMENT_SOURCE_RUNTIME_STATE_UNSPECIFIED
	}
}

func durationSecondsToUint32(d time.Duration) uint32 {
	const maxUint32 = int64(1<<32 - 1)
	seconds := int64(d / time.Second)
	if seconds <= 0 {
		return 0
	}
	if seconds > maxUint32 {
		return math.MaxUint32
	}
	return uint32(seconds) // #nosec G115 -- bounds-checked above
}

func intToInt32Saturating(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	if n < math.MinInt32 {
		return math.MinInt32
	}
	return int32(n) // #nosec G115 -- bounds-checked above
}

func mqttTimeProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func durationFromSeconds(seconds uint32) time.Duration {
	if seconds == 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func validateMqttUpdateClears(req *pb.UpdateMqttCurtailmentSourceRequest) error {
	if req.GetClearContractedCurtailmentKw() && req.ContractedCurtailmentKw != nil {
		return fleeterror.NewInvalidArgumentError("clear_contracted_curtailment_kw conflicts with contracted_curtailment_kw")
	}
	if req.GetClearStalenessThresholdSec() && req.StalenessThresholdSec != nil {
		return fleeterror.NewInvalidArgumentError("clear_staleness_threshold_sec conflicts with staleness_threshold_sec")
	}
	if req.GetClearMinCurtailedDurationSec() && req.MinCurtailedDurationSec != nil {
		return fleeterror.NewInvalidArgumentError("clear_min_curtailed_duration_sec conflicts with min_curtailed_duration_sec")
	}
	return nil
}
