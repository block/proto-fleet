// Package curtailment wires the curtailment RPC surface.
package curtailment

import (
	"context"

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
const listCurtailmentEventsDefaultPageSize int32 = 50
const listCurtailmentEventsMaxPageSize int32 = 200
const listCurtailmentEventsMaxPermissionScanPages = 3

const requestReadMaxBytes = 2 << 20

// Handler implements the curtailment RPC surface; service=nil keeps
// RPC bodies at Unimplemented after any entry auth gates run.
type Handler struct {
	service          *curtailment.Service
	mqttSettings     *mqttingest.SettingsService
	responseProfiles *curtailment.ResponseProfileService
	automation       *curtailment.AutomationService
}

var _ curtailmentv1connect.CurtailmentServiceHandler = &Handler{}

// RequestReadLimitOption caps curtailment RPC bodies before Connect unmarshals them.
func RequestReadLimitOption() connect.HandlerOption {
	return connect.WithReadMaxBytes(requestReadMaxBytes)
}

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
	info, err := requireScopedPermissionCapability(ctx, authz.PermCurtailmentManage)
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
	if req.Msg.CandidateMinPowerWOverride != nil || req.Msg.GetForceIncludeAllPairedMiners() {
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
	info, err := requireScopedPermissionCapability(ctx, authz.PermCurtailmentManage)
	if err != nil {
		return nil, err
	}
	startReq, err := toStartRequest(req.Msg, info)
	if err != nil {
		return nil, err
	}
	if h.service != nil {
		replayEvent, err := h.service.LookupStartReplay(ctx, startReq)
		if err != nil {
			return nil, err
		}
		if replayEvent != nil {
			if err := h.requirePersistedEventPermission(ctx, info.OrganizationID, authz.PermCurtailmentManage, replayEvent); err != nil {
				return nil, err
			}
			plan, err := h.service.RenderStartReplay(ctx, info.OrganizationID, replayEvent)
			if err != nil {
				return nil, err
			}
			return connect.NewResponse(&pb.StartCurtailmentResponse{
				Event: toEventProtoWithTargets(plan.ReplayEvent, plan.ReplayTargets),
			}), nil
		}
	}
	requirements, err := h.startResourceContextRequirements(ctx, info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	info, err = requireScopeResourceContextPermissions(ctx, authz.PermCurtailmentManage, requirements, info)
	if err != nil {
		return nil, err
	}
	authorizedFans, err := h.authorizeFacilityFanDevices(ctx, info.OrganizationID, req.Msg.GetFacilityFanDeviceIds())
	if err != nil {
		return nil, err
	}
	if req.Msg.CandidateMinPowerWOverride != nil ||
		req.Msg.AllowUnbounded ||
		req.Msg.ForceIncludeMaintenance ||
		req.Msg.GetForceIncludeAllPairedMiners() {
		// force_include_maintenance is safety-critical (curtails miners
		// under physical maintenance), so the same admin gate applies.
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("StartCurtailment")
	}

	startReq.AuthorizedDeviceSites = requirements.deviceSites
	startReq.AuthorizedFanSites = make(map[int64]int64, len(authorizedFans))
	for deviceID, device := range authorizedFans {
		startReq.AuthorizedFanSites[deviceID] = device.SiteID
	}

	plan, err := h.service.Start(ctx, startReq)
	if err != nil {
		return nil, err
	}

	if plan.InsufficientLoadDetail != nil {
		return nil, toInsufficientLoadError(plan.InsufficientLoadDetail)
	}
	if plan.ReplayEvent != nil {
		if err := h.requirePersistedEventPermission(ctx, info.OrganizationID, authz.PermCurtailmentManage, plan.ReplayEvent); err != nil {
			return nil, err
		}
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
	info, permissionEvent, err := h.requireEventPermission(ctx, authz.PermCurtailmentManage, eventUUID)
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
	copyEventTargetSiteCoverage(event, permissionEvent)
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
	info, permissionEvent, err := h.requireEventPermission(ctx, authz.PermCurtailmentManage, eventUUID)
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
	copyEventTargetSiteCoverage(event, permissionEvent)
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
	info, err := requireScopedPermissionCapability(ctx, authz.PermCurtailmentRead)
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
	if err := h.hydrateTargetSiteCoverageByEvents(ctx, info.OrganizationID, events); err != nil {
		return nil, err
	}
	// Envelope filtering removes whole-org events for narrowed callers. Keep the
	// renderer gate as defense in depth because their live rollups aggregate
	// target counts across every site.
	orgWideRead, err := hasOrgWidePermission(ctx, authz.PermCurtailmentRead)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListActiveCurtailmentsResponse(events, orgWideRead)), nil
}

// hasOrgWidePermission reports whether the caller holds permission at org
// scope without site narrowing; Forbidden maps to false rather than failing
// the request.
func hasOrgWidePermission(ctx context.Context, permission string) (bool, error) {
	if _, err := middleware.RequireOrgWidePermission(ctx, permission); err != nil {
		if fleeterror.IsForbiddenError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (h *Handler) ListCurtailmentEvents(ctx context.Context, req *connect.Request[pb.ListCurtailmentEventsRequest]) (*connect.Response[pb.ListCurtailmentEventsResponse], error) {
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("ListCurtailmentEvents")
	}
	info, err := requireScopedPermissionCapability(ctx, authz.PermCurtailmentRead)
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
	if err := h.hydrateTargetSiteCoverageByEvents(ctx, info.OrganizationID, events); err != nil {
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
	info, permissionEvent, err := h.requireEventPermission(ctx, authz.PermCurtailmentRead, eventUUID)
	if err != nil {
		return nil, err
	}
	if err := h.hydrateTargetSiteCoverageByEvent(ctx, info.OrganizationID, permissionEvent); err != nil {
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
	copyEventTargetSiteCoverage(event, permissionEvent)
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
	info, permissionEvent, err := h.requireEventPermission(ctx, authz.PermCurtailmentManage, eventUUID)
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
	copyEventTargetSiteCoverage(event, permissionEvent)
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
// capability before loading the event envelope so recovery remains scoped to
// the durable authorization snapshot.
func (h *Handler) ForceReleaseCurtailmentOwnership(ctx context.Context, req *connect.Request[pb.ForceReleaseCurtailmentOwnershipRequest]) (*connect.Response[pb.ForceReleaseCurtailmentOwnershipResponse], error) {
	info, err := requireScopedPermissionCapability(ctx, authz.PermCurtailmentManage)
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
	copyEventTargetSiteCoverage(result.Event, event)
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
	deviceSites    map[string]*int64
}

type authorizationEnvelopeRequirements struct {
	resource        scopeResourceContextRequirements
	facilityFanRead scopeResourceContextRequirements
}

type authorizationEnvelopePermissionCache struct {
	resource        resourceContextPermissionCache
	facilityFanRead resourceContextPermissionCache
}

func newAuthorizationEnvelopePermissionCache() *authorizationEnvelopePermissionCache {
	return &authorizationEnvelopePermissionCache{
		resource:        newResourceContextPermissionCache(),
		facilityFanRead: newResourceContextPermissionCache(),
	}
}

func authorizationEnvelopeResourceContextRequirements(raw []byte) (authorizationEnvelopeRequirements, error) {
	envelope, err := curtailment.AuthorizationEnvelopeFromJSON(raw)
	if err != nil {
		return authorizationEnvelopeRequirements{}, fleeterror.NewInternalErrorf(
			"invalid persisted curtailment authorization envelope: %v", err,
		)
	}
	minerSiteIDs := append([]int64(nil), envelope.SelectedResourceSiteIDs...)
	minerSiteIDs = append(minerSiteIDs, envelope.CurrentMemberSiteIDs...)
	minerContexts := siteResourceContextsForScope(curtailment.Scope{SiteIDs: minerSiteIDs})
	fanContexts := siteResourceContextsForScope(curtailment.Scope{SiteIDs: envelope.FacilityFanSiteIDs})
	return authorizationEnvelopeRequirements{
		resource: scopeResourceContextRequirements{
			siteContexts:   mergeSiteResourceContexts(minerContexts, fanContexts),
			requireOrgWide: envelope.MinerScopeUnbounded || envelope.FacilityFanScopeUnbounded,
		},
		facilityFanRead: scopeResourceContextRequirements{
			siteContexts:   fanContexts,
			requireOrgWide: envelope.FacilityFanScopeUnbounded,
		},
	}, nil
}

func requireAuthorizationEnvelopePermissions(ctx context.Context, permission string, raw []byte) error {
	requirements, err := authorizationEnvelopeResourceContextRequirements(raw)
	if err != nil {
		return err
	}
	if err := requireResourceContextPermissions(ctx, permission, requirements.resource); err != nil {
		return err
	}
	return requireResourceContextPermissions(ctx, authz.PermSiteRead, requirements.facilityFanRead)
}

func authorizationEnvelopeAccessAllowed(
	ctx context.Context,
	permission string,
	raw []byte,
	cache *authorizationEnvelopePermissionCache,
) (bool, error) {
	requirements, err := authorizationEnvelopeResourceContextRequirements(raw)
	if err != nil {
		return false, err
	}
	allowed, err := resourceContextRequirementsAllowed(
		ctx,
		permission,
		requirements.resource,
		&cache.resource,
	)
	if err != nil || !allowed {
		return allowed, err
	}
	return resourceContextRequirementsAllowed(
		ctx,
		authz.PermSiteRead,
		requirements.facilityFanRead,
		&cache.facilityFanRead,
	)
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
	scope, err := toTerminalScope(scopes)
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
	if len(scope.BuildingIDs) > 0 || len(scope.RackIDs) > 0 || len(scope.GroupIDs) > 0 {
		// The persisted envelope secures later reads and recovery. Live topology
		// starts still require org-wide access until dispatch-time reauthorization
		// can bind the resolved membership to every physical command.
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
	out.deviceSites = deviceSites
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
		len(scope.BuildingIDs) == 0 &&
		len(scope.RackIDs) == 0 &&
		len(scope.GroupIDs) == 0 &&
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

func mergeSiteResourceContexts(groups ...[]authz.ResourceContext) []authz.ResourceContext {
	var siteIDs []int64
	for _, group := range groups {
		for _, rc := range group {
			if rc.SiteID != nil {
				siteIDs = append(siteIDs, *rc.SiteID)
			}
		}
	}
	return siteResourceContextsForScope(curtailment.Scope{SiteIDs: siteIDs})
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

func requireScopedPermissionCapability(ctx context.Context, permission string) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	orgWide, siteIDs, err := middleware.SiteScopeForPermission(ctx, permission)
	if err != nil {
		return nil, err
	}
	if orgWide || len(siteIDs) > 0 {
		return info, nil
	}
	// Preserve the middleware's structured permission-denied response when the
	// caller has no grant for this capability at any supported scope.
	return middleware.RequirePermission(ctx, permission, authz.ResourceContext{})
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
	info, err := requireScopedPermissionCapability(ctx, permission)
	if err != nil {
		return nil, nil, err
	}
	event, err := h.service.GetEvent(ctx, info.OrganizationID, eventUUID)
	if err != nil {
		return nil, nil, err
	}
	if err := requireAuthorizationEnvelopePermissions(ctx, permission, event.AuthorizationEnvelopeJSON); err != nil {
		return nil, nil, err
	}
	return info, event, nil
}

func copyEventTargetSiteCoverage(dst, src *models.Event) {
	if dst == nil || src == nil || src.TargetSiteCoverage == nil {
		return
	}
	coverage := *src.TargetSiteCoverage
	coverage.SiteIDs = append([]int64(nil), src.TargetSiteCoverage.SiteIDs...)
	dst.TargetSiteCoverage = &coverage
}

func (h *Handler) requireForceReleasePermission(ctx context.Context, orgID int64, event *models.Event) error {
	return h.requirePersistedEventPermission(ctx, orgID, authz.PermCurtailmentManage, event)
}

func (h *Handler) requirePersistedEventPermission(ctx context.Context, orgID int64, permission string, event *models.Event) error {
	if event == nil || event.OrgID != orgID {
		return fleeterror.NewNotFoundError("curtailment event not found")
	}
	return requireAuthorizationEnvelopePermissions(ctx, permission, event.AuthorizationEnvelopeJSON)
}

func (h *Handler) filterEventsByPermission(
	ctx context.Context,
	orgID int64,
	permission string,
	events []*models.Event,
) ([]*models.Event, error) {
	filtered := make([]*models.Event, 0, len(events))
	cache := newAuthorizationEnvelopePermissionCache()
	for _, event := range events {
		if event == nil || event.OrgID != orgID {
			continue
		}
		permitted, err := authorizationEnvelopeAccessAllowed(ctx, permission, event.AuthorizationEnvelopeJSON, cache)
		if err != nil {
			return nil, err
		}
		if permitted {
			filtered = append(filtered, event)
		}
	}
	return filtered, nil
}

func (h *Handler) hydrateTargetSiteCoverageByEvents(ctx context.Context, orgID int64, events []*models.Event) error {
	eventUUIDs := make([]uuid.UUID, 0, len(events))
	seen := make(map[uuid.UUID]struct{}, len(events))
	for _, event := range events {
		if !shouldHydrateTargetSiteCoverage(event) {
			continue
		}
		if _, ok := seen[event.EventUUID]; ok {
			continue
		}
		seen[event.EventUUID] = struct{}{}
		eventUUIDs = append(eventUUIDs, event.EventUUID)
	}
	if len(eventUUIDs) == 0 {
		return nil
	}
	coverageByEvent, err := h.service.ListTargetSiteCoverageByEvents(ctx, orgID, eventUUIDs)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event == nil {
			continue
		}
		if coverage, ok := coverageByEvent[event.EventUUID]; ok {
			event.TargetSiteCoverage = &coverage
		}
	}
	return nil
}

func (h *Handler) hydrateTargetSiteCoverageByEvent(ctx context.Context, orgID int64, event *models.Event) error {
	if !shouldHydrateTargetSiteCoverage(event) {
		return nil
	}
	coverage, err := h.service.ListTargetSiteCoverageByEvent(ctx, orgID, event.EventUUID)
	if err != nil {
		return err
	}
	event.TargetSiteCoverage = &coverage
	return nil
}

func shouldHydrateTargetSiteCoverage(event *models.Event) bool {
	if event == nil || event.TargetSiteCoverage != nil {
		return false
	}
	switch event.ScopeType {
	case models.ScopeTypeDeviceList:
		return true
	case models.ScopeTypeMixed:
		scope, hasScope, err := curtailment.ScopeFromJSON(event.ScopeJSON)
		return err == nil && (!hasScope || !curtailment.IsSiteOnlyScope(scope))
	case models.ScopeTypeWholeOrg, models.ScopeTypeSite, "":
		return false
	}
	return false
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
