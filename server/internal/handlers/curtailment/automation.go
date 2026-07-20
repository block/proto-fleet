package curtailment

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func (h *Handler) ListCurtailmentAutomationRules(ctx context.Context, _ *connect.Request[pb.ListCurtailmentAutomationRulesRequest]) (*connect.Response[pb.ListCurtailmentAutomationRulesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.automation == nil {
		return nil, errCurtailmentNotImplemented("ListCurtailmentAutomationRules")
	}
	rules, err := h.automation.List(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	if h.responseProfiles == nil {
		return nil, errCurtailmentNotImplemented("ListCurtailmentAutomationRules")
	}
	profiles, err := h.responseProfiles.List(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	profilesByID := make(map[int64]*models.ResponseProfile, len(profiles))
	for _, profile := range profiles {
		if profile != nil {
			profilesByID[profile.ID] = profile
		}
	}
	referencedProfiles := make([]*models.ResponseProfile, 0, len(rules))
	seenProfileIDs := make(map[int64]struct{}, len(rules))
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		if _, seen := seenProfileIDs[rule.ResponseProfileID]; seen {
			continue
		}
		if profile := profilesByID[rule.ResponseProfileID]; profile != nil {
			referencedProfiles = append(referencedProfiles, profile)
			seenProfileIDs[rule.ResponseProfileID] = struct{}{}
		}
	}
	out := make([]*pb.CurtailmentAutomationRule, 0, len(rules))
	deviceSites, err := h.responseProfileDeviceSitesForAutomationRules(ctx, info.OrganizationID, rules)
	if err != nil {
		return nil, err
	}
	facilityFanDeviceSites, err := h.responseProfileFacilityFanDeviceSitesForProfiles(ctx, info.OrganizationID, referencedProfiles)
	if err != nil {
		return nil, err
	}
	siteAllowed := make(map[int64]bool)
	facilityFanSiteAllowed := make(map[int64]bool)
	orgWideAllowed := false
	orgWideChecked := false
	for _, rule := range rules {
		requirements, err := h.automationRuleProfileResourceContextRequirements(ctx, info.OrganizationID, rule, deviceSites)
		if err != nil {
			return nil, err
		}
		allowed, err := resourceContextRequirementsAllowed(
			ctx,
			authz.PermCurtailmentManage,
			requirements,
			siteAllowed,
			&orgWideAllowed,
			&orgWideChecked,
		)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		profile := profilesByID[rule.ResponseProfileID]
		if profile == nil {
			continue
		}
		allowed, err = facilityFanSiteAccessAllowed(
			ctx,
			profile.FacilityFanDeviceIDs,
			facilityFanDeviceSites,
			facilityFanSiteAllowed,
		)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		out = append(out, toAutomationRuleProto(rule))
	}
	return connect.NewResponse(&pb.ListCurtailmentAutomationRulesResponse{Rules: out}), nil
}

func (h *Handler) GetCurtailmentAutomationRule(ctx context.Context, req *connect.Request[pb.GetCurtailmentAutomationRuleRequest]) (*connect.Response[pb.GetCurtailmentAutomationRuleResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.automation == nil {
		return nil, errCurtailmentNotImplemented("GetCurtailmentAutomationRule")
	}
	rule, err := h.getAutomationRuleWithProfilePermission(ctx, info.OrganizationID, req.Msg.GetRuleId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetCurtailmentAutomationRuleResponse{Rule: toAutomationRuleProto(rule)}), nil
}

func (h *Handler) CreateCurtailmentAutomationRule(ctx context.Context, req *connect.Request[pb.CreateCurtailmentAutomationRuleRequest]) (*connect.Response[pb.CreateCurtailmentAutomationRuleResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.automation == nil {
		return nil, errCurtailmentNotImplemented("CreateCurtailmentAutomationRule")
	}
	profile, err := h.getResponseProfileWithSitePermission(ctx, info.OrganizationID, req.Msg.GetResponseProfileId())
	if err != nil {
		return nil, err
	}
	rule := automationRuleFromCreateRequest(info.OrganizationID, req.Msg)
	created, err := h.automation.Create(ctx, domainCurtailment.SaveAutomationRuleRequest{
		Rule:                               rule,
		CanUseAdminControls:                canUseAdminControls(info),
		ExpectedResponseProfileFanSettings: responseProfileFanSettings(profile),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateCurtailmentAutomationRuleResponse{Rule: toAutomationRuleProto(created)}), nil
}

func (h *Handler) UpdateCurtailmentAutomationRule(ctx context.Context, req *connect.Request[pb.UpdateCurtailmentAutomationRuleRequest]) (*connect.Response[pb.UpdateCurtailmentAutomationRuleResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.automation == nil {
		return nil, errCurtailmentNotImplemented("UpdateCurtailmentAutomationRule")
	}
	if _, err := h.getAutomationRuleWithProfilePermission(ctx, info.OrganizationID, req.Msg.GetRuleId()); err != nil {
		return nil, err
	}
	profile, err := h.getResponseProfileWithSitePermission(ctx, info.OrganizationID, req.Msg.GetResponseProfileId())
	if err != nil {
		return nil, err
	}
	rule := automationRuleFromUpdateRequest(info.OrganizationID, req.Msg)
	updated, err := h.automation.Update(ctx, domainCurtailment.SaveAutomationRuleRequest{
		Rule:                               rule,
		CanUseAdminControls:                canUseAdminControls(info),
		ExpectedResponseProfileFanSettings: responseProfileFanSettings(profile),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateCurtailmentAutomationRuleResponse{Rule: toAutomationRuleProto(updated)}), nil
}

func (h *Handler) SetCurtailmentAutomationRuleEnabled(ctx context.Context, req *connect.Request[pb.SetCurtailmentAutomationRuleEnabledRequest]) (*connect.Response[pb.SetCurtailmentAutomationRuleEnabledResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.automation == nil {
		return nil, errCurtailmentNotImplemented("SetCurtailmentAutomationRuleEnabled")
	}
	_, profile, err := h.getAutomationRuleWithAuthorizedProfile(ctx, info.OrganizationID, req.Msg.GetRuleId())
	if err != nil {
		return nil, err
	}
	rule, err := h.automation.SetEnabled(
		ctx,
		info.OrganizationID,
		req.Msg.GetRuleId(),
		req.Msg.GetEnabled(),
		canUseAdminControls(info),
		responseProfileFanSettings(profile),
	)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetCurtailmentAutomationRuleEnabledResponse{Rule: toAutomationRuleProto(rule)}), nil
}

func (h *Handler) DeleteCurtailmentAutomationRule(ctx context.Context, req *connect.Request[pb.DeleteCurtailmentAutomationRuleRequest]) (*connect.Response[pb.DeleteCurtailmentAutomationRuleResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.automation == nil {
		return nil, errCurtailmentNotImplemented("DeleteCurtailmentAutomationRule")
	}
	if _, err := h.getAutomationRuleWithProfilePermission(ctx, info.OrganizationID, req.Msg.GetRuleId()); err != nil {
		return nil, err
	}
	if err := h.automation.Delete(ctx, info.OrganizationID, req.Msg.GetRuleId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteCurtailmentAutomationRuleResponse{}), nil
}

func (h *Handler) getAutomationRuleWithProfilePermission(ctx context.Context, orgID, ruleID int64) (*models.AutomationRule, error) {
	rule, _, err := h.getAutomationRuleWithAuthorizedProfile(ctx, orgID, ruleID)
	return rule, err
}

func (h *Handler) getAutomationRuleWithAuthorizedProfile(ctx context.Context, orgID, ruleID int64) (*models.AutomationRule, *models.ResponseProfile, error) {
	rule, err := h.automation.Get(ctx, orgID, ruleID)
	if err != nil {
		return nil, nil, err
	}
	profile, err := h.requireAutomationRuleProfilePermission(ctx, rule)
	if err != nil {
		return nil, nil, err
	}
	return rule, profile, nil
}

func (h *Handler) requireAutomationRuleProfilePermission(ctx context.Context, rule *models.AutomationRule) (*models.ResponseProfile, error) {
	if rule == nil {
		return nil, nil
	}
	requirements, err := h.automationRuleProfileResourceContextRequirements(ctx, rule.OrgID, rule, nil)
	if err != nil {
		return nil, err
	}
	if err := requireResourceContextPermissions(ctx, authz.PermCurtailmentManage, requirements); err != nil {
		return nil, err
	}
	if h.responseProfiles == nil {
		return nil, errCurtailmentNotImplemented("GetCurtailmentAutomationRule")
	}
	profile, err := h.responseProfiles.Get(ctx, rule.OrgID, rule.ResponseProfileID)
	if err != nil {
		return nil, err
	}
	if err := h.requireFacilityFanSitePermissions(ctx, rule.OrgID, profile.FacilityFanDeviceIDs); err != nil {
		return nil, err
	}
	return profile, nil
}

func (h *Handler) responseProfileDeviceSitesForAutomationRules(
	ctx context.Context,
	orgID int64,
	rules []*models.AutomationRule,
) (map[string]*int64, error) {
	var deviceIdentifiers []string
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		scope, err := domainCurtailment.ResponseProfileScope(models.ResponseProfile{
			SiteID:    rule.ResponseProfileSiteID,
			ScopeJSON: rule.ResponseProfileScopeJSON,
		})
		if err != nil {
			return nil, err
		}
		deviceIdentifiers = append(deviceIdentifiers, scope.DeviceIdentifiers...)
	}
	deviceIdentifiers = uniqueResponseProfileDeviceIdentifiers(deviceIdentifiers)
	if len(deviceIdentifiers) == 0 {
		return map[string]*int64{}, nil
	}
	if h.responseProfiles == nil {
		return nil, nil
	}
	return h.responseProfiles.ListDeviceSites(ctx, orgID, deviceIdentifiers)
}

func (h *Handler) automationRuleProfileResourceContextRequirements(
	ctx context.Context,
	orgID int64,
	rule *models.AutomationRule,
	deviceSites map[string]*int64,
) (scopeResourceContextRequirements, error) {
	if rule == nil {
		return scopeResourceContextRequirements{}, nil
	}
	return h.responseProfileResourceContextRequirements(ctx, orgID, &models.ResponseProfile{
		SiteID:    rule.ResponseProfileSiteID,
		ScopeJSON: rule.ResponseProfileScopeJSON,
	}, deviceSites, false)
}

func automationRuleFromCreateRequest(orgID int64, msg *pb.CreateCurtailmentAutomationRuleRequest) models.AutomationRule {
	enabled := true
	if msg.Enabled != nil {
		enabled = msg.GetEnabled()
	}
	return models.AutomationRule{
		OrgID:             orgID,
		RuleName:          msg.GetRuleName(),
		TriggerType:       automationTriggerTypeFromProto(msg.GetTriggerType()),
		MQTTSourceID:      msg.GetMqttSourceId(),
		ResponseProfileID: msg.GetResponseProfileId(),
		Enabled:           enabled,
	}
}

func automationRuleFromUpdateRequest(orgID int64, msg *pb.UpdateCurtailmentAutomationRuleRequest) models.AutomationRule {
	return models.AutomationRule{
		ID:                msg.GetRuleId(),
		OrgID:             orgID,
		RuleName:          msg.GetRuleName(),
		TriggerType:       automationTriggerTypeFromProto(msg.GetTriggerType()),
		MQTTSourceID:      msg.GetMqttSourceId(),
		ResponseProfileID: msg.GetResponseProfileId(),
	}
}

func automationTriggerTypeFromProto(v pb.CurtailmentAutomationTriggerType) models.AutomationTriggerType {
	switch v {
	case pb.CurtailmentAutomationTriggerType_CURTAILMENT_AUTOMATION_TRIGGER_TYPE_MQTT,
		pb.CurtailmentAutomationTriggerType_CURTAILMENT_AUTOMATION_TRIGGER_TYPE_UNSPECIFIED:
		return models.AutomationTriggerTypeMQTT
	default:
		return models.AutomationTriggerType(v.String())
	}
}

func automationTriggerTypeProto(v models.AutomationTriggerType) pb.CurtailmentAutomationTriggerType {
	switch v {
	case models.AutomationTriggerTypeMQTT:
		return pb.CurtailmentAutomationTriggerType_CURTAILMENT_AUTOMATION_TRIGGER_TYPE_MQTT
	default:
		return pb.CurtailmentAutomationTriggerType_CURTAILMENT_AUTOMATION_TRIGGER_TYPE_UNSPECIFIED
	}
}

func automationSignalProto(v *models.AutomationSignal) pb.CurtailmentAutomationSignal {
	if v == nil {
		return pb.CurtailmentAutomationSignal_CURTAILMENT_AUTOMATION_SIGNAL_UNSPECIFIED
	}
	switch *v {
	case models.AutomationSignalOff:
		return pb.CurtailmentAutomationSignal_CURTAILMENT_AUTOMATION_SIGNAL_OFF
	case models.AutomationSignalOn:
		return pb.CurtailmentAutomationSignal_CURTAILMENT_AUTOMATION_SIGNAL_ON
	default:
		return pb.CurtailmentAutomationSignal_CURTAILMENT_AUTOMATION_SIGNAL_UNSPECIFIED
	}
}

func toAutomationRuleProto(rule *models.AutomationRule) *pb.CurtailmentAutomationRule {
	if rule == nil {
		return nil
	}
	out := &pb.CurtailmentAutomationRule{
		RuleId:              rule.ID,
		RuleName:            rule.RuleName,
		TriggerType:         automationTriggerTypeProto(rule.TriggerType),
		MqttSourceId:        rule.MQTTSourceID,
		MqttSourceName:      rule.MQTTSourceName,
		ResponseProfileId:   rule.ResponseProfileID,
		ResponseProfileName: rule.ResponseProfileName,
		Enabled:             rule.Enabled,
		CurrentSignal:       automationSignalProto(rule.LastSignal),
		CreatedAt:           timestamppb.New(rule.CreatedAt),
		UpdatedAt:           timestamppb.New(rule.UpdatedAt),
	}
	if rule.LastSignalAt != nil {
		out.LastSignalAt = timestamppb.New(*rule.LastSignalAt)
	}
	if rule.ActiveEventUUID != nil {
		out.ActiveEventUuid = rule.ActiveEventUUID.String()
	}
	if rule.LastStartedAt != nil {
		out.LastStartedAt = timestamppb.New(*rule.LastStartedAt)
	}
	if rule.LastRestoredAt != nil {
		out.LastRestoredAt = timestamppb.New(*rule.LastRestoredAt)
	}
	if rule.LastError != nil {
		out.LastError = *rule.LastError
	}
	if rule.LastErrorAt != nil {
		out.LastErrorAt = timestamppb.New(*rule.LastErrorAt)
	}
	return out
}
