// Package curtailment wires the curtailment RPC surface.
package curtailment

import (
	"context"
	"encoding/json"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Action verb for requireAdminFromContext error messages on the legacy
// admin-only override checks that run after the curtailment:manage gate.
const actionSupplyOverrideFields = "supply curtailment override fields"
const actionAdminTerminateEvents = "admin terminate curtailment events"
const actionManageMqttSources = "manage MaestroOS curtailment sources"
const incompleteTargetSiteContextMessage = "curtailment target site context is incomplete"
const listCurtailmentEventsDefaultPageSize int32 = 50
const listCurtailmentEventsMaxPageSize int32 = 200
const listCurtailmentEventsMaxPermissionScanPages = 3

// Handler implements the curtailment RPC surface; service=nil keeps
// RPC bodies at Unimplemented after any entry auth gates run.
type Handler struct {
	service          *curtailment.Service
	mqttSettings     *mqttingest.SettingsService
	responseProfiles *curtailment.ResponseProfileService
	automation       *curtailment.AutomationService
}

var _ curtailmentv1connect.CurtailmentServiceHandler = &Handler{}

func NewHandler(service *curtailment.Service, mqttSettings ...*mqttingest.SettingsService) *Handler {
	h := &Handler{service: service}
	if len(mqttSettings) > 0 {
		h.mqttSettings = mqttSettings[0]
	}
	return h
}

func NewHandlerWithResponseProfiles(
	service *curtailment.Service,
	profiles *curtailment.ResponseProfileService,
	mqttSettings ...*mqttingest.SettingsService,
) *Handler {
	h := NewHandler(service, mqttSettings...)
	h.responseProfiles = profiles
	return h
}

func NewHandlerWithAutomation(
	service *curtailment.Service,
	profiles *curtailment.ResponseProfileService,
	automation *curtailment.AutomationService,
	mqttSettings *mqttingest.SettingsService,
) *Handler {
	h := NewHandlerWithResponseProfiles(service, profiles, mqttSettings)
	h.automation = automation
	return h
}

func (h *Handler) PreviewCurtailmentPlan(ctx context.Context, req *connect.Request[pb.PreviewCurtailmentPlanRequest]) (*connect.Response[pb.PreviewCurtailmentPlanResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	requirements, err := h.previewResourceContextRequirements(ctx, info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	info, err = requireScopeResourceContextPermissions(ctx, authz.PermCurtailmentManage, requirements, info)
	if err != nil {
		return nil, err
	}
	if req.Msg.CandidateMinPowerWOverride != nil {
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("PreviewCurtailmentPlan")
	}

	previewReq, err := toPreviewRequest(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	plan, err := h.service.Preview(ctx, previewReq)
	if err != nil {
		return nil, err
	}

	if plan.InsufficientLoadDetail != nil {
		return nil, toInsufficientLoadError(plan.InsufficientLoadDetail)
	}

	return connect.NewResponse(toPreviewResponse(plan, req.Msg)), nil
}

func (h *Handler) StartCurtailment(ctx context.Context, req *connect.Request[pb.StartCurtailmentRequest]) (*connect.Response[pb.StartCurtailmentResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	requirements, err := h.startResourceContextRequirements(ctx, info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	info, err = requireScopeResourceContextPermissions(ctx, authz.PermCurtailmentManage, requirements, info)
	if err != nil {
		return nil, err
	}
	if req.Msg.CandidateMinPowerWOverride != nil || req.Msg.AllowUnbounded || req.Msg.ForceIncludeMaintenance {
		// force_include_maintenance is safety-critical (curtails miners
		// under physical maintenance), so the same admin gate applies.
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("StartCurtailment")
	}

	startReq, err := toStartRequest(req.Msg, info)
	if err != nil {
		return nil, err
	}

	plan, err := h.service.Start(ctx, startReq)
	if err != nil {
		return nil, err
	}

	if plan.InsufficientLoadDetail != nil {
		return nil, toInsufficientLoadError(plan.InsufficientLoadDetail)
	}
	if plan.ReplayEvent != nil {
		return connect.NewResponse(&pb.StartCurtailmentResponse{
			Event: toEventProtoWithTargets(plan.ReplayEvent, plan.ReplayTargets),
		}), nil
	}

	return connect.NewResponse(toStartResponse(plan, req.Msg)), nil
}

func (h *Handler) UpdateCurtailmentEvent(ctx context.Context, req *connect.Request[pb.UpdateCurtailmentEventRequest]) (*connect.Response[pb.UpdateCurtailmentEventResponse], error) {
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("UpdateCurtailmentEvent")
	}
	eventUUID, err := parseEventUUID(req.Msg.GetEventUuid())
	if err != nil {
		return nil, err
	}
	info, _, err := h.requireEventPermission(ctx, authz.PermCurtailmentManage, eventUUID)
	if err != nil {
		return nil, err
	}
	updateReq, err := toUpdateRequest(req.Msg, info)
	if err != nil {
		return nil, err
	}
	updateReq.CanUseAdminControls = canUseAdminControls(info)
	event, err := h.service.Update(ctx, updateReq)
	if err != nil {
		return nil, err
	}
	targets, err := h.service.ListTargetsByEvent(ctx, info.OrganizationID, event.EventUUID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateCurtailmentEventResponse{
		Event: toEventProtoWithTargets(event, targets),
	}), nil
}

func (h *Handler) StopCurtailment(ctx context.Context, req *connect.Request[pb.StopCurtailmentRequest]) (*connect.Response[pb.StopCurtailmentResponse], error) {
	if req.Msg.GetForce() {
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	if h.service == nil {
		if _, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{}); err != nil {
			return nil, err
		}
		return nil, errCurtailmentNotImplemented("StopCurtailment")
	}
	eventUUID, err := parseEventUUID(req.Msg.GetEventUuid())
	if err != nil {
		return nil, err
	}
	info, _, err := h.requireEventPermission(ctx, authz.PermCurtailmentManage, eventUUID)
	if err != nil {
		return nil, err
	}

	stopReq, err := toStopRequest(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	event, err := h.service.Stop(ctx, stopReq)
	if err != nil {
		return nil, err
	}
	targets, err := h.service.ListTargetsByEvent(ctx, info.OrganizationID, event.EventUUID)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.StopCurtailmentResponse{
		Event: toEventProtoWithTargets(event, targets),
	}), nil
}

func (h *Handler) ListActiveCurtailments(ctx context.Context, _ *connect.Request[pb.ListActiveCurtailmentsRequest]) (*connect.Response[pb.ListActiveCurtailmentsResponse], error) {
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("ListActiveCurtailments")
	}
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	events, err := h.service.ListActive(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	events, err = h.filterEventsByPermission(ctx, info.OrganizationID, authz.PermCurtailmentRead, events)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListActiveCurtailmentsResponse(events)), nil
}

func (h *Handler) ListCurtailmentEvents(ctx context.Context, req *connect.Request[pb.ListCurtailmentEventsRequest]) (*connect.Response[pb.ListCurtailmentEventsResponse], error) {
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("ListCurtailmentEvents")
	}
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	listReq, err := toListEventsRequest(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	events, nextToken, err := h.listPermittedEvents(ctx, listReq)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListEventsResponse(events, nextToken)), nil
}

func (h *Handler) GetCurtailmentEvent(ctx context.Context, req *connect.Request[pb.GetCurtailmentEventRequest]) (*connect.Response[pb.GetCurtailmentEventResponse], error) {
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("GetCurtailmentEvent")
	}
	eventUUID, err := parseEventUUID(req.Msg.GetEventUuid())
	if err != nil {
		return nil, err
	}
	info, _, err := h.requireEventPermission(ctx, authz.PermCurtailmentRead, eventUUID)
	if err != nil {
		return nil, err
	}
	event, targets, nextTargetPageToken, err := h.service.GetEventWithTargets(ctx, curtailment.GetEventWithTargetsRequest{
		OrgID:           info.OrganizationID,
		EventUUID:       eventUUID,
		TargetPageSize:  req.Msg.GetTargetPageSize(),
		TargetPageToken: req.Msg.GetTargetPageToken(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetCurtailmentEventResponse{
		Event:               toEventProtoWithTargets(event, targets),
		NextTargetPageToken: nextTargetPageToken,
	}), nil
}

// AdminTerminateEvent forces a non-terminal event to terminal. Paired
// with SessionOnlyProcedures (see interceptors/config.go); callers need
// curtailment:manage for the event plus an Admin/SuperAdmin role.
func (h *Handler) AdminTerminateEvent(ctx context.Context, req *connect.Request[pb.AdminTerminateEventRequest]) (*connect.Response[pb.AdminTerminateEventResponse], error) {
	if h.service == nil {
		if _, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{}); err != nil {
			return nil, err
		}
		if err := requireAdminFromContext(ctx, actionAdminTerminateEvents); err != nil {
			return nil, err
		}
		return nil, errCurtailmentNotImplemented("AdminTerminateEvent")
	}
	eventUUID, err := parseEventUUID(req.Msg.GetEventUuid())
	if err != nil {
		return nil, err
	}
	info, _, err := h.requireEventPermission(ctx, authz.PermCurtailmentManage, eventUUID)
	if err != nil {
		return nil, err
	}
	if err := requireAdminFromContext(ctx, actionAdminTerminateEvents); err != nil {
		return nil, err
	}
	terminateReq, err := toAdminTerminateRequest(req.Msg, info)
	if err != nil {
		return nil, err
	}
	event, err := h.service.AdminTerminate(ctx, terminateReq)
	if err != nil {
		return nil, err
	}
	targets, err := h.service.ListTargetsByEvent(ctx, info.OrganizationID, event.EventUUID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AdminTerminateEventResponse{
		Event: toEventProtoWithTargets(event, targets),
	}), nil
}

// ForceReleaseCurtailmentOwnership is an admin recovery path that releases
// curtailment ownership immediately. It intentionally checks org-level manage
// permission before loading event site contexts so incomplete target-site
// coverage cannot block recovery.
func (h *Handler) ForceReleaseCurtailmentOwnership(ctx context.Context, req *connect.Request[pb.ForceReleaseCurtailmentOwnershipRequest]) (*connect.Response[pb.ForceReleaseCurtailmentOwnershipResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if err := requireAdminFromContext(ctx, actionAdminTerminateEvents); err != nil {
		return nil, err
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("ForceReleaseCurtailmentOwnership")
	}
	eventUUID, err := parseEventUUID(req.Msg.GetEventUuid())
	if err != nil {
		return nil, err
	}
	event, err := h.service.GetEvent(ctx, info.OrganizationID, eventUUID)
	if err != nil {
		return nil, err
	}
	if err := h.requireForceReleasePermission(ctx, info.OrganizationID, event); err != nil {
		return nil, err
	}
	forceReq, err := toForceReleaseRequest(req.Msg, info, eventUUID)
	if err != nil {
		return nil, err
	}
	result, err := h.service.ForceRelease(ctx, forceReq)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ForceReleaseCurtailmentOwnershipResponse{
		Event:               toForceReleaseEventProto(result.Event),
		ReleasedTargetCount: uint32SaturatingInt64(result.ReleasedTargetCount),
		OwnershipReleased:   result.OwnershipReleased,
		AutomationDisabled:  result.AutomationDisabled,
	}), nil
}

// IngestCurtailmentSignal starts a curtailment event from an external
// dispatch signal. Permission gate runs before the body so denial
// surfaces regardless of whether the body has shipped.
func (h *Handler) IngestCurtailmentSignal(ctx context.Context, _ *connect.Request[pb.IngestCurtailmentSignalRequest]) (*connect.Response[pb.IngestCurtailmentSignalResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermCurtailmentIngest, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return nil, errCurtailmentNotImplemented("IngestCurtailmentSignal")
}

func errCurtailmentNotImplemented(rpc string) error {
	return fleeterror.NewUnimplementedErrorf("curtailment.%s is not implemented yet", rpc)
}

type scopeResourceContextRequirements struct {
	siteContexts   []authz.ResourceContext
	requireOrgWide bool
}

func (h *Handler) previewResourceContextRequirements(
	ctx context.Context,
	orgID int64,
	msg *pb.PreviewCurtailmentPlanRequest,
) (scopeResourceContextRequirements, error) {
	if scopes := msg.GetScopes(); len(scopes) > 0 {
		return h.scopeResourceContextRequirementsFromProto(ctx, orgID, scopes, nil, false)
	}
	switch s := msg.GetScope().(type) {
	case *pb.PreviewCurtailmentPlanRequest_WholeOrg:
		return scopeResourceContextRequirements{requireOrgWide: true}, nil
	case *pb.PreviewCurtailmentPlanRequest_Site:
		siteID := s.Site.GetSiteId()
		return scopeResourceContextRequirements{siteContexts: []authz.ResourceContext{{SiteID: &siteID}}}, nil
	case *pb.PreviewCurtailmentPlanRequest_DeviceIdentifiers:
		scope := curtailment.Scope{DeviceIdentifiers: s.DeviceIdentifiers.GetDeviceIdentifiers()}
		return h.scopeResourceContextRequirements(ctx, orgID, scope, nil, false)
	}
	return scopeResourceContextRequirements{}, nil
}

func (h *Handler) startResourceContextRequirements(
	ctx context.Context,
	orgID int64,
	msg *pb.StartCurtailmentRequest,
) (scopeResourceContextRequirements, error) {
	if scopes := msg.GetScopes(); len(scopes) > 0 {
		return h.scopeResourceContextRequirementsFromProto(ctx, orgID, scopes, nil, false)
	}
	switch s := msg.GetScope().(type) {
	case *pb.StartCurtailmentRequest_WholeOrg:
		return scopeResourceContextRequirements{requireOrgWide: true}, nil
	case *pb.StartCurtailmentRequest_Site:
		siteID := s.Site.GetSiteId()
		return scopeResourceContextRequirements{siteContexts: []authz.ResourceContext{{SiteID: &siteID}}}, nil
	case *pb.StartCurtailmentRequest_DeviceIdentifiers:
		scope := curtailment.Scope{DeviceIdentifiers: s.DeviceIdentifiers.GetDeviceIdentifiers()}
		return h.scopeResourceContextRequirements(ctx, orgID, scope, nil, false)
	}
	return scopeResourceContextRequirements{}, nil
}

func (h *Handler) scopeResourceContextRequirementsFromProto(
	ctx context.Context,
	orgID int64,
	scopes []*pb.CurtailmentScope,
	deviceSites map[string]*int64,
	requireKnownDevices bool,
) (scopeResourceContextRequirements, error) {
	scope, err := toCompositeScope(scopes)
	if err != nil {
		return scopeResourceContextRequirements{}, err
	}
	return h.scopeResourceContextRequirements(ctx, orgID, scope, deviceSites, requireKnownDevices)
}

func (h *Handler) scopeResourceContextRequirements(
	ctx context.Context,
	orgID int64,
	scope curtailment.Scope,
	deviceSites map[string]*int64,
	requireKnownDevices bool,
) (scopeResourceContextRequirements, error) {
	out := scopeResourceContextRequirements{
		siteContexts: siteResourceContextsForScope(scope),
	}
	if scope.Type == models.ScopeTypeWholeOrg || scopeHasNoSelectors(scope) {
		out.requireOrgWide = true
		return out, nil
	}
	deviceIdentifiers := uniqueResponseProfileDeviceIdentifiers(scope.DeviceIdentifiers)
	if len(deviceIdentifiers) == 0 {
		return out, nil
	}
	if deviceSites == nil {
		if h.responseProfiles == nil {
			out.requireOrgWide = true
			return out, nil
		}
		var err error
		deviceSites, err = h.responseProfiles.ListDeviceSites(ctx, orgID, deviceIdentifiers)
		if err != nil {
			return scopeResourceContextRequirements{}, err
		}
	}
	siteIDs := siteIDsFromResourceContexts(out.siteContexts)
	for _, deviceIdentifier := range deviceIdentifiers {
		siteID, ok := deviceSites[deviceIdentifier]
		if !ok {
			if requireKnownDevices {
				return scopeResourceContextRequirements{}, fleeterror.NewNotFoundError("one or more device identifiers were not found")
			}
			out.requireOrgWide = true
			continue
		}
		if siteID == nil {
			out.requireOrgWide = true
			continue
		}
		siteIDs = append(siteIDs, *siteID)
	}
	out.siteContexts = siteResourceContextsForScope(curtailment.Scope{SiteIDs: siteIDs})
	return out, nil
}

func scopeHasNoSelectors(scope curtailment.Scope) bool {
	return scope.Type == "" &&
		scope.SiteID == 0 &&
		len(scope.SiteIDs) == 0 &&
		len(scope.DeviceSetIDs) == 0 &&
		len(scope.DeviceIdentifiers) == 0
}

func siteResourceContextsForScope(scope curtailment.Scope) []authz.ResourceContext {
	siteIDs := append([]int64(nil), scope.SiteIDs...)
	if scope.SiteID != 0 {
		siteIDs = append(siteIDs, scope.SiteID)
	}
	if len(siteIDs) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(siteIDs))
	out := make([]authz.ResourceContext, 0, len(siteIDs))
	for _, siteID := range siteIDs {
		if siteID == 0 {
			continue
		}
		if _, ok := seen[siteID]; ok {
			continue
		}
		seen[siteID] = struct{}{}
		out = append(out, authz.ResourceContext{SiteID: &siteID})
	}
	return out
}

func requireOrgPermissionWithOptionalSiteContexts(ctx context.Context, permission string, siteContexts []authz.ResourceContext) (*session.Info, error) {
	info, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	for _, rc := range siteContexts {
		if rc.SiteID == nil {
			continue
		}
		checkedInfo, err := middleware.RequirePermission(ctx, permission, rc)
		if err != nil {
			return nil, err
		}
		info = checkedInfo
	}
	return info, nil
}

func requireScopeResourceContextPermissions(
	ctx context.Context,
	permission string,
	requirements scopeResourceContextRequirements,
	info *session.Info,
) (*session.Info, error) {
	if requirements.requireOrgWide {
		checkedInfo, err := middleware.RequireOrgWidePermission(ctx, permission)
		if err != nil {
			return nil, err
		}
		info = checkedInfo
	}
	for _, rc := range requirements.siteContexts {
		if rc.SiteID == nil {
			continue
		}
		checkedInfo, err := middleware.RequirePermission(ctx, permission, rc)
		if err != nil {
			return nil, err
		}
		info = checkedInfo
	}
	return info, nil
}

func requireResourceContextPermissions(ctx context.Context, permission string, requirements scopeResourceContextRequirements) error {
	_, err := requireScopeResourceContextPermissions(ctx, permission, requirements, nil)
	return err
}

func parseEventUUID(raw string) (uuid.UUID, error) {
	eventUUID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fleeterror.NewInvalidArgumentErrorf(
			"event_uuid must be a valid UUID: %v", err,
		)
	}
	return eventUUID, nil
}

func (h *Handler) requireEventPermission(ctx context.Context, permission string, eventUUID uuid.UUID) (*session.Info, *models.Event, error) {
	info, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{})
	if err != nil {
		return nil, nil, err
	}
	event, err := h.service.GetEvent(ctx, info.OrganizationID, eventUUID)
	if err != nil {
		return nil, nil, err
	}
	siteContexts, err := h.eventSiteResourceContexts(ctx, info.OrganizationID, event)
	if err != nil {
		if isIncompleteTargetSiteContextError(err) {
			info, err = middleware.RequireOrgWidePermission(ctx, permission)
			if err != nil {
				return nil, nil, err
			}
			return info, event, nil
		}
		return nil, nil, err
	}
	for _, rc := range siteContexts {
		checkedInfo, err := middleware.RequirePermission(ctx, permission, rc)
		if err != nil {
			return nil, nil, err
		}
		info = checkedInfo
	}
	return info, event, nil
}

func (h *Handler) requireForceReleasePermission(ctx context.Context, orgID int64, event *models.Event) error {
	siteContexts, err := h.eventSiteResourceContexts(ctx, orgID, event)
	if err != nil {
		if isIncompleteTargetSiteContextError(err) {
			_, err := middleware.RequireOrgWidePermission(ctx, authz.PermCurtailmentManage)
			return err
		}
		return err
	}
	for _, rc := range siteContexts {
		if _, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, rc); err != nil {
			return err
		}
	}
	if len(siteContexts) == 0 {
		_, err := middleware.RequireOrgWidePermission(ctx, authz.PermCurtailmentManage)
		return err
	}
	return nil
}

func (h *Handler) filterEventsByPermission(
	ctx context.Context,
	orgID int64,
	permission string,
	events []*models.Event,
) ([]*models.Event, error) {
	filtered := make([]*models.Event, 0, len(events))
	for _, event := range events {
		siteContexts, err := h.eventSiteResourceContexts(ctx, orgID, event)
		if err != nil {
			if isIncompleteTargetSiteContextError(err) {
				if _, orgWideErr := middleware.RequireOrgWidePermission(ctx, permission); orgWideErr != nil {
					if fleeterror.IsForbiddenError(orgWideErr) {
						continue
					}
					return nil, orgWideErr
				}
				filtered = append(filtered, event)
				continue
			}
			if fleeterror.IsForbiddenError(err) {
				continue
			}
			return nil, err
		}
		permitted := true
		for _, rc := range siteContexts {
			if _, err := middleware.RequirePermission(ctx, permission, rc); err != nil {
				if fleeterror.IsForbiddenError(err) {
					permitted = false
					break
				}
				return nil, err
			}
		}
		if permitted {
			filtered = append(filtered, event)
		}
	}
	return filtered, nil
}

func (h *Handler) listPermittedEvents(
	ctx context.Context,
	req curtailment.ListEventsRequest,
) ([]*models.Event, string, error) {
	pageSize := normalizedListCurtailmentEventsPageSize(req.PageSize)
	filtered := make([]*models.Event, 0, pageSize)
	nextReq := req
	nextReq.PageSize = pageSize

	for range listCurtailmentEventsMaxPermissionScanPages {
		nextReq.PageSize = remainingListCurtailmentEventsPageSize(pageSize, len(filtered))
		events, nextToken, err := h.service.ListEvents(ctx, nextReq)
		if err != nil {
			return nil, "", err
		}
		permitted, err := h.filterEventsByPermission(ctx, req.OrgID, authz.PermCurtailmentRead, events)
		if err != nil {
			return nil, "", err
		}
		filtered = append(filtered, permitted...)
		if len(filtered) == int(pageSize) || nextToken == "" {
			return filtered, nextToken, nil
		}
		nextReq.PageToken = nextToken
	}
	return filtered, nextReq.PageToken, nil
}

func normalizedListCurtailmentEventsPageSize(pageSize int32) int32 {
	if pageSize <= 0 {
		return listCurtailmentEventsDefaultPageSize
	}
	if pageSize > listCurtailmentEventsMaxPageSize {
		return listCurtailmentEventsMaxPageSize
	}
	return pageSize
}

func remainingListCurtailmentEventsPageSize(pageSize int32, filteredCount int) int32 {
	remaining := int(pageSize) - filteredCount
	if remaining <= 0 {
		return 0
	}
	if remaining > int(listCurtailmentEventsMaxPageSize) {
		return listCurtailmentEventsMaxPageSize
	}
	return int32(remaining) // #nosec G115 -- page size is clamped to <= 200 above.
}

func requireOrgPermissionWithOptionalSiteContext(ctx context.Context, permission string, rc authz.ResourceContext) (*session.Info, error) {
	return requireOrgPermissionWithOptionalSiteContexts(ctx, permission, []authz.ResourceContext{rc})
}

func eventResourceContext(event *models.Event) (authz.ResourceContext, error) {
	if event == nil || event.ScopeType != models.ScopeTypeSite {
		return authz.ResourceContext{}, nil
	}
	var payload struct {
		SiteID int64 `json:"site_id"`
	}
	if err := json.Unmarshal(event.ScopeJSON, &payload); err != nil {
		return authz.ResourceContext{}, fleeterror.NewInternalErrorf(
			"failed to decode site-scoped curtailment event scope: %v", err,
		)
	}
	if payload.SiteID <= 0 {
		return authz.ResourceContext{}, fleeterror.NewInternalError(
			"site-scoped curtailment event has invalid site_id",
		)
	}
	return authz.ResourceContext{SiteID: &payload.SiteID}, nil
}

func (h *Handler) eventSiteResourceContexts(
	ctx context.Context,
	orgID int64,
	event *models.Event,
) ([]authz.ResourceContext, error) {
	rc, err := eventResourceContext(event)
	if err != nil || rc.SiteID != nil {
		if rc.SiteID == nil {
			return nil, err
		}
		return []authz.ResourceContext{rc}, err
	}
	if event == nil || event.ScopeType == "" || event.ScopeType == models.ScopeTypeWholeOrg {
		return nil, nil
	}
	if contexts, handled, err := mixedSiteOnlyEventResourceContexts(event); handled || err != nil {
		return contexts, err
	}
	siteIDs, complete, err := h.service.ListTargetSiteIDsByEvent(ctx, orgID, event.EventUUID)
	if err != nil {
		return nil, err
	}
	if !complete {
		return nil, fleeterror.NewForbiddenError(incompleteTargetSiteContextMessage)
	}
	if len(siteIDs) == 0 {
		if contexts, handled, err := h.scopeJSONEventResourceContexts(ctx, orgID, event); handled || err != nil {
			return contexts, err
		}
	}
	contexts := make([]authz.ResourceContext, 0, len(siteIDs))
	for _, siteID := range siteIDs {
		contexts = append(contexts, authz.ResourceContext{SiteID: &siteID})
	}
	return contexts, nil
}

func mixedSiteOnlyEventResourceContexts(event *models.Event) ([]authz.ResourceContext, bool, error) {
	if event == nil || event.ScopeType != models.ScopeTypeMixed {
		return nil, false, nil
	}
	scope, hasScope, err := curtailment.ScopeFromJSON(event.ScopeJSON)
	if err != nil {
		return nil, true, fleeterror.NewInternalErrorf(
			"failed to decode mixed curtailment event scope: %v", err,
		)
	}
	if !hasScope || !curtailment.IsSiteOnlyScope(scope) {
		return nil, false, nil
	}
	contexts := siteResourceContextsForScope(scope)
	if len(contexts) == 0 {
		return nil, true, fleeterror.NewInternalError("mixed site-only curtailment event has no site_ids")
	}
	return contexts, true, nil
}

func (h *Handler) scopeJSONEventResourceContexts(
	ctx context.Context,
	orgID int64,
	event *models.Event,
) ([]authz.ResourceContext, bool, error) {
	if event == nil || len(event.ScopeJSON) == 0 {
		return nil, false, nil
	}
	scope, hasScope, err := curtailment.ScopeFromJSON(event.ScopeJSON)
	if err != nil {
		return nil, true, fleeterror.NewInternalErrorf(
			"failed to decode curtailment event scope: %v", err,
		)
	}
	if !hasScope {
		return nil, false, nil
	}
	requirements, err := h.scopeResourceContextRequirements(ctx, orgID, scope, nil, false)
	if err != nil {
		return nil, true, err
	}
	if requirements.requireOrgWide {
		return nil, true, fleeterror.NewForbiddenError(incompleteTargetSiteContextMessage)
	}
	return requirements.siteContexts, true, nil
}

func isIncompleteTargetSiteContextError(err error) bool {
	return fleeterror.IsForbiddenError(err) && strings.Contains(err.Error(), incompleteTargetSiteContextMessage)
}

// requireAdminFromContext returns Forbidden unless the caller has Admin
// or SuperAdmin role.
func requireAdminFromContext(ctx context.Context, action string) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return fleeterror.NewUnauthenticatedError("authentication required")
	}
	if !canUseAdminControls(info) {
		return fleeterror.NewForbiddenErrorf("only admins can %s", action)
	}
	return nil
}

func canUseAdminControls(info *session.Info) bool {
	return info != nil &&
		(info.Role == domainAuth.SuperAdminRoleName || info.Role == domainAuth.AdminRoleName)
}
