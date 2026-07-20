package curtailment

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	maxAutomationRuleNameLength = 64

	automationExternalSource        = "curtailment_automation"
	automationRuleIdempotencyPrefix = "curtailment_automation_rule:"
)

// AutomationService validates automation rule CRUD and executes MQTT trigger
// edges against response profiles.
type AutomationService struct {
	store       interfaces.AutomationStore
	profiles    *ResponseProfileService
	sourceStore mqttingest.SettingsStore
	curtailment *Service
	clock       func() time.Time
}

type AutomationServiceConfig struct {
	Store       interfaces.AutomationStore
	Profiles    *ResponseProfileService
	SourceStore mqttingest.SettingsStore
	Curtailment *Service
	Clock       func() time.Time
}

func NewAutomationService(cfg AutomationServiceConfig) (*AutomationService, error) {
	if cfg.Store == nil {
		return nil, errors.New("curtailment automation: store is required")
	}
	if cfg.Profiles == nil {
		return nil, errors.New("curtailment automation: response profile service is required")
	}
	if cfg.SourceStore == nil {
		return nil, errors.New("curtailment automation: MQTT source store is required")
	}
	if cfg.Curtailment == nil {
		return nil, errors.New("curtailment automation: curtailment service is required")
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &AutomationService{
		store:       cfg.Store,
		profiles:    cfg.Profiles,
		sourceStore: cfg.SourceStore,
		curtailment: cfg.Curtailment,
		clock:       cfg.Clock,
	}, nil
}

type SaveAutomationRuleRequest struct {
	Rule                               models.AutomationRule
	CanUseAdminControls                bool
	ExpectedResponseProfileFanSettings models.ResponseProfileFanSettings
}

func (s *AutomationService) List(ctx context.Context, orgID int64) ([]*models.AutomationRule, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	return s.store.ListAutomationRules(ctx, orgID)
}

func (s *AutomationService) Get(ctx context.Context, orgID, ruleID int64) (*models.AutomationRule, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if ruleID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("rule_id must be set")
	}
	return s.store.GetAutomationRule(ctx, orgID, ruleID)
}

func (s *AutomationService) Create(ctx context.Context, req SaveAutomationRuleRequest) (*models.AutomationRule, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	rule, expectedFanSettings, err := s.validateAndNormalize(
		ctx,
		req.Rule,
		req.CanUseAdminControls,
		req.ExpectedResponseProfileFanSettings,
	)
	if err != nil {
		return nil, err
	}
	return s.store.CreateAutomationRule(ctx, rule, expectedFanSettings)
}

func (s *AutomationService) Update(ctx context.Context, req SaveAutomationRuleRequest) (*models.AutomationRule, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	if req.Rule.ID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("rule_id must be set")
	}
	rule, expectedFanSettings, err := s.validateAndNormalize(
		ctx,
		req.Rule,
		req.CanUseAdminControls,
		req.ExpectedResponseProfileFanSettings,
	)
	if err != nil {
		return nil, err
	}
	existing, err := s.store.GetAutomationRule(ctx, rule.OrgID, rule.ID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureNoNonTerminalActiveEvent(ctx, existing, "update"); err != nil {
		return nil, err
	}
	return s.store.UpdateAutomationRule(ctx, rule, expectedFanSettings)
}

func (s *AutomationService) SetEnabled(
	ctx context.Context,
	orgID int64,
	ruleID int64,
	enabled bool,
	canUseAdminControls bool,
	expectedFanSettings models.ResponseProfileFanSettings,
) (*models.AutomationRule, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if ruleID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("rule_id must be set")
	}
	rule, err := s.store.GetAutomationRule(ctx, orgID, ruleID)
	if err != nil {
		return nil, err
	}
	if enabled {
		var err error
		expectedFanSettings, err = s.ensureProfileCanBeAutomated(ctx, rule, canUseAdminControls, expectedFanSettings)
		if err != nil {
			return nil, err
		}
	}
	if !enabled {
		if err := s.ensureNoNonTerminalActiveEvent(ctx, rule, "disable"); err != nil {
			return nil, err
		}
	}
	return s.store.SetAutomationRuleEnabled(ctx, orgID, ruleID, enabled, expectedFanSettings)
}

func (s *AutomationService) Delete(ctx context.Context, orgID, ruleID int64) error {
	if err := s.ensureConfigured(); err != nil {
		return err
	}
	if orgID <= 0 {
		return fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if ruleID <= 0 {
		return fleeterror.NewInvalidArgumentError("rule_id must be set")
	}
	rule, err := s.store.GetAutomationRule(ctx, orgID, ruleID)
	if err != nil {
		return err
	}
	if err := s.ensureNoNonTerminalActiveEvent(ctx, rule, "delete"); err != nil {
		return err
	}
	return s.store.DeleteAutomationRule(ctx, orgID, ruleID)
}

// HandleMQTTSignal executes enabled automation rules for an MQTT edge. Returning
// an error tells mqttingest to keep the pending edge for retry.
func (s *AutomationService) HandleMQTTSignal(ctx context.Context, signal mqttingest.SignalEdge) error {
	if err := s.ensureConfigured(); err != nil {
		return err
	}
	rules, err := s.store.ListEnabledAutomationRulesByMQTTSource(ctx, signal.Source.ID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, rule := range rules {
		if err := s.handleRuleSignal(ctx, rule, signal); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if recordErr := s.store.RecordAutomationExecutionError(ctx, rule.ID, err.Error(), s.clock()); recordErr != nil {
				if firstErr == nil {
					firstErr = recordErr
				}
			}
		}
	}
	return firstErr
}

func (s *AutomationService) handleRuleSignal(ctx context.Context, rule *models.AutomationRule, signal mqttingest.SignalEdge) error {
	if rule == nil {
		return nil
	}
	at := signal.ReceivedAt
	if at.IsZero() {
		at = s.clock()
	}
	normalized, err := automationSignalFromMQTTTarget(signal.Target)
	if err != nil {
		return err
	}
	coalesce, err := s.shouldCoalesceRepeatedOff(ctx, rule, signal, normalized, at)
	if err != nil {
		return err
	}
	if coalesce {
		return nil
	}
	if err := s.store.RecordAutomationSignal(ctx, rule.ID, normalized, at); err != nil {
		return err
	}
	switch normalized {
	case models.AutomationSignalOff:
		return s.handleRuleOff(ctx, rule, signal, at)
	case models.AutomationSignalOn:
		return s.handleRuleOn(ctx, rule, at)
	default:
		return fleeterror.NewInvalidArgumentErrorf("unsupported automation signal %q", normalized)
	}
}

func (s *AutomationService) shouldCoalesceRepeatedOff(
	ctx context.Context,
	rule *models.AutomationRule,
	signal mqttingest.SignalEdge,
	normalized models.AutomationSignal,
	at time.Time,
) (bool, error) {
	if rule == nil ||
		signal.Direction != mqttingest.EdgeReassertOff ||
		normalized != models.AutomationSignalOff ||
		rule.ActiveEventUUID == nil ||
		rule.LastSignal == nil ||
		*rule.LastSignal != models.AutomationSignalOff ||
		rule.LastSignalAt == nil {
		return false, nil
	}
	if at.Sub(*rule.LastSignalAt) >= mqttingest.RepeatedOffMinInterval {
		return false, nil
	}
	event, err := s.curtailment.GetEvent(ctx, rule.OrgID, *rule.ActiveEventUUID)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	return event != nil && !event.State.IsTerminal() && event.State != models.EventStateRestoring, nil
}

func eventMaxDurationElapsed(event *models.Event, now time.Time) bool {
	if event == nil ||
		event.AllowUnbounded ||
		event.MaxDurationSeconds == nil ||
		*event.MaxDurationSeconds <= 0 ||
		event.StartedAt == nil {
		return false
	}
	return now.Sub(*event.StartedAt) >= time.Duration(*event.MaxDurationSeconds)*time.Second
}

func (s *AutomationService) handleRuleOff(ctx context.Context, rule *models.AutomationRule, signal mqttingest.SignalEdge, at time.Time) error {
	if rule.ActiveEventUUID != nil {
		event, err := s.curtailment.GetEvent(ctx, rule.OrgID, *rule.ActiveEventUUID)
		if err != nil && !fleeterror.IsNotFoundError(err) {
			return err
		}
		switch {
		case event == nil:
			// Stale state; start a fresh event below.
		case event.State.IsTerminal():
			// Stale terminal state; start a fresh event below.
		case event.State == models.EventStateRestoring:
			if eventMaxDurationElapsed(event, s.clock()) {
				return nil
			}
			recurtailed, err := s.curtailment.Recurtail(ctx, RecurtailRequest{
				OrgID:     rule.OrgID,
				EventUUID: event.EventUUID,
			})
			if err != nil {
				return err
			}
			return s.store.SetAutomationActiveEvent(ctx, rule.ID, signal.Source.ID, recurtailed.EventUUID, at)
		default:
			return nil
		}
	}

	profile, err := s.profiles.Get(ctx, rule.OrgID, rule.ResponseProfileID)
	if err != nil {
		return err
	}
	startReq, err := startRequestFromAutomationProfile(rule, profile, signal)
	if err != nil {
		return err
	}
	if startReq.Mode == models.ModeFullFleet {
		startReq.AllowUnbounded = true
		startReq.MaxDurationSeconds = nil
	}
	plan, err := s.curtailment.Start(ctx, startReq)
	if err != nil {
		return err
	}
	if plan.InsufficientLoadDetail != nil {
		return fleeterror.NewFailedPreconditionError("automation response profile could not start curtailment: insufficient curtailable load")
	}
	if plan.EventUUID == nil {
		return fleeterror.NewInternalError("automation response profile start did not return an event UUID")
	}
	if plan.ReplayEvent != nil {
		if err := validateAutomationReplayEvent(plan.ReplayEvent, rule, profile); err != nil {
			return err
		}
	}
	if err := s.store.SetAutomationActiveEvent(ctx, rule.ID, signal.Source.ID, *plan.EventUUID, at); err != nil {
		if fleeterror.IsFailedPreconditionError(err) {
			if _, releaseErr := s.curtailment.ForceRelease(ctx, ForceReleaseRequest{
				OrgID:     rule.OrgID,
				EventUUID: *plan.EventUUID,
				Reason:    "automation rule disabled before active event could be recorded",
			}); releaseErr != nil {
				return fmt.Errorf("%w; failed to release untracked automation event: %v", err, releaseErr)
			}
		}
		return err
	}
	return nil
}

func (s *AutomationService) handleRuleOn(ctx context.Context, rule *models.AutomationRule, at time.Time) error {
	event, err := s.restoreCandidateEvent(ctx, rule)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return s.store.ClearAutomationActiveEvent(ctx, rule.ID, at)
		}
		return err
	}
	if event == nil || event.State.IsTerminal() {
		if rule.ActiveEventUUID != nil {
			return s.store.ClearAutomationActiveEvent(ctx, rule.ID, at)
		}
		return nil
	}
	_, err = s.curtailment.Stop(ctx, StopRequest{
		OrgID:             rule.OrgID,
		EventUUID:         event.EventUUID,
		AutomationRestore: true,
	})
	if err != nil {
		return err
	}
	return s.store.RecordAutomationRestoreStarted(ctx, rule.ID, at)
}

func (s *AutomationService) restoreCandidateEvent(ctx context.Context, rule *models.AutomationRule) (*models.Event, error) {
	if rule.ActiveEventUUID != nil {
		return s.curtailment.GetEvent(ctx, rule.OrgID, *rule.ActiveEventUUID)
	}
	externalReference, idempotencyKey := automationRuleEventReference(rule.ID)
	event, err := s.curtailment.store.GetEventByIdempotencyKey(ctx, rule.OrgID, idempotencyKey)
	if err != nil || event != nil {
		return event, validateAutomationReplayOwnership(event, rule, err)
	}
	event, err = s.curtailment.store.GetEventByExternalReference(ctx, rule.OrgID, automationExternalSource, externalReference)
	return event, validateAutomationReplayOwnership(event, rule, err)
}

func (s *AutomationService) ensureNoNonTerminalActiveEvent(ctx context.Context, rule *models.AutomationRule, action string) error {
	if rule == nil || rule.ActiveEventUUID == nil {
		return nil
	}
	event, err := s.curtailment.GetEvent(ctx, rule.OrgID, *rule.ActiveEventUUID)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return s.store.ClearAutomationActiveEvent(ctx, rule.ID, s.clock())
		}
		return err
	}
	if event == nil || event.State.IsTerminal() {
		return s.store.ClearAutomationActiveEvent(ctx, rule.ID, s.clock())
	}
	return fleeterror.NewFailedPreconditionErrorf(
		"cannot %s curtailment automation rule while automation event %s is %s; restore or complete the event first",
		action,
		event.EventUUID,
		event.State,
	)
}

func (s *AutomationService) validateAndNormalize(
	ctx context.Context,
	rule models.AutomationRule,
	canUseAdminControls bool,
	expectedFanSettings models.ResponseProfileFanSettings,
) (models.AutomationRule, models.ResponseProfileFanSettings, error) {
	rule.RuleName = strings.TrimSpace(rule.RuleName)
	if rule.OrgID <= 0 {
		return models.AutomationRule{}, models.ResponseProfileFanSettings{}, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if err := validateAutomationRuleName(rule.RuleName); err != nil {
		return models.AutomationRule{}, models.ResponseProfileFanSettings{}, err
	}
	if rule.TriggerType == "" {
		rule.TriggerType = models.AutomationTriggerTypeMQTT
	}
	if rule.TriggerType != models.AutomationTriggerTypeMQTT {
		return models.AutomationRule{}, models.ResponseProfileFanSettings{}, fleeterror.NewInvalidArgumentErrorf("trigger_type %q is not supported; only MQTT (MaestroOS source) is supported", rule.TriggerType)
	}
	if rule.MQTTSourceID <= 0 {
		return models.AutomationRule{}, models.ResponseProfileFanSettings{}, fleeterror.NewInvalidArgumentError("mqtt_source_id must be set")
	}
	if rule.ResponseProfileID <= 0 {
		return models.AutomationRule{}, models.ResponseProfileFanSettings{}, fleeterror.NewInvalidArgumentError("response_profile_id must be set")
	}
	if _, err := s.sourceStore.GetSourceConfigByOrg(ctx, rule.OrgID, rule.MQTTSourceID); err != nil {
		return models.AutomationRule{}, models.ResponseProfileFanSettings{}, mqttSourceLookupError(err)
	}
	profile, err := s.profiles.Get(ctx, rule.OrgID, rule.ResponseProfileID)
	if err != nil {
		return models.AutomationRule{}, models.ResponseProfileFanSettings{}, err
	}
	if err := validateAutomationProfileBinding(profile, canUseAdminControls); err != nil {
		return models.AutomationRule{}, models.ResponseProfileFanSettings{}, err
	}
	if !sameResponseProfileFanSettings(responseProfileFanSettings(profile), expectedFanSettings) {
		return models.AutomationRule{}, models.ResponseProfileFanSettings{}, fleeterror.NewFailedPreconditionError(
			"curtailment response profile changed before automation rule save; retry",
		)
	}
	return rule, expectedFanSettings, nil
}

func (s *AutomationService) ensureProfileCanBeAutomated(
	ctx context.Context,
	rule *models.AutomationRule,
	canUseAdminControls bool,
	expectedFanSettings models.ResponseProfileFanSettings,
) (models.ResponseProfileFanSettings, error) {
	if rule == nil {
		return models.ResponseProfileFanSettings{}, nil
	}
	profile, err := s.profiles.Get(ctx, rule.OrgID, rule.ResponseProfileID)
	if err != nil {
		return models.ResponseProfileFanSettings{}, err
	}
	if err := validateAutomationProfileBinding(profile, canUseAdminControls); err != nil {
		return models.ResponseProfileFanSettings{}, err
	}
	if !sameResponseProfileFanSettings(responseProfileFanSettings(profile), expectedFanSettings) {
		return models.ResponseProfileFanSettings{}, fleeterror.NewFailedPreconditionError(
			"curtailment response profile changed before automation rule save; retry",
		)
	}
	return expectedFanSettings, nil
}

func validateAutomationProfileBinding(profile *models.ResponseProfile, canUseAdminControls bool) error {
	if profile == nil {
		return nil
	}
	if canUseAdminControls || !responseProfileRequiresAdminControls(*profile) {
		return nil
	}
	return fleeterror.NewForbiddenError("only admins can bind automation rules to response profiles with admin-only controls")
}

func responseProfileFanSettings(profile *models.ResponseProfile) models.ResponseProfileFanSettings {
	if profile == nil {
		return models.ResponseProfileFanSettings{}
	}
	return models.ResponseProfileFanSettings{
		FacilityFanDeviceIDs: append([]int64(nil), profile.FacilityFanDeviceIDs...),
		FanOffDelaySec:       profile.FanOffDelaySec,
		FanRestoreDelaySec:   profile.FanRestoreDelaySec,
	}
}

func sameResponseProfileFanSettings(a, b models.ResponseProfileFanSettings) bool {
	return slices.Equal(a.FacilityFanDeviceIDs, b.FacilityFanDeviceIDs) &&
		a.FanOffDelaySec == b.FanOffDelaySec &&
		a.FanRestoreDelaySec == b.FanRestoreDelaySec
}

func (s *AutomationService) ensureConfigured() error {
	if s == nil || s.store == nil || s.profiles == nil || s.sourceStore == nil || s.curtailment == nil {
		return fleeterror.NewUnimplementedError("curtailment automation service is not configured")
	}
	return nil
}

func validateAutomationRuleName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fleeterror.NewInvalidArgumentError("rule_name is required")
	}
	if n := utf8.RuneCountInString(name); n > maxAutomationRuleNameLength {
		return fleeterror.NewInvalidArgumentErrorf(
			"rule_name must be at most %d characters, got %d",
			maxAutomationRuleNameLength,
			n,
		)
	}
	return nil
}

func mqttSourceLookupError(err error) error {
	if errors.Is(err, mqttingest.ErrSourceConfigNotFound) {
		return fleeterror.NewNotFoundError("MaestroOS source not found")
	}
	return err
}

func automationSignalFromMQTTTarget(target mqttingest.Target) (models.AutomationSignal, error) {
	switch target {
	case mqttingest.TargetUnknown:
		return "", fleeterror.NewInvalidArgumentError("unsupported MaestroOS target \"unknown\"")
	case mqttingest.TargetOff:
		return models.AutomationSignalOff, nil
	case mqttingest.TargetOn:
		return models.AutomationSignalOn, nil
	default:
		return "", fleeterror.NewInvalidArgumentErrorf("unsupported MaestroOS target %q", target.String())
	}
}

func startRequestFromAutomationProfile(rule *models.AutomationRule, profile *models.ResponseProfile, signal mqttingest.SignalEdge) (StartRequest, error) {
	scope, err := ResponseProfileScope(*profile)
	if err != nil {
		return StartRequest{}, fleeterror.NewInvalidArgumentErrorf("invalid response profile scope for automation rule %d: %v", rule.ID, err)
	}
	targetKW := float64Value(profile.TargetKW)
	toleranceKW := float64Value(profile.ToleranceKW)
	externalReference, idempotencyKey := automationRuleEventReference(rule.ID)
	sourceActorID := externalReference
	reason := fmt.Sprintf("Automation %q from MaestroOS source %q", rule.RuleName, signal.Source.SourceName)
	return StartRequest{
		PreviewRequest: PreviewRequest{
			OrgID:                       rule.OrgID,
			Scope:                       scope,
			Mode:                        profile.Mode,
			Strategy:                    profile.Strategy,
			Level:                       profile.Level,
			Priority:                    profile.Priority,
			TargetKW:                    targetKW,
			ToleranceKW:                 toleranceKW,
			IncludeMaintenance:          profile.IncludeMaintenance,
			ForceIncludeMaintenance:     profile.ForceIncludeMaintenance,
			ForceIncludeAllPairedMiners: profile.ForceIncludeAllPairedMiners,
			// MQTT demand-response signals must execute immediately; profile
			// cooldown applies only to non-emergency user-driven starts.
			PostEventCooldownSec: 0,
		},
		Reason:                    reason,
		RestoreBatchSize:          profile.RestoreBatchSize,
		RestoreBatchIntervalSec:   profile.RestoreBatchIntervalSec,
		CurtailBatchSize:          cloneInt32Ptr(profile.CurtailBatchSize),
		CurtailBatchIntervalSec:   profile.CurtailBatchIntervalSec,
		UseProfileCurtailSettings: true,
		FacilityFanDeviceIDs:      append([]int64(nil), profile.FacilityFanDeviceIDs...),
		FanOffDelaySec:            profile.FanOffDelaySec,
		FanRestoreDelaySec:        profile.FanRestoreDelaySec,
		IdempotencyKey:            &idempotencyKey,
		ExternalSource:            stringPtr(automationExternalSource),
		ExternalReference:         &externalReference,
		SourceActorType:           models.SourceActorAutomation,
		SourceActorID:             &sourceActorID,
		CreatedByUserID:           signal.Source.ServiceUserID,
		// Automation rule create/update/enable validates that profiles using
		// admin-only controls are admin-authorized before MQTT can execute them.
		CanUseAdminControls: true,
	}, nil
}

func automationRuleEventReference(ruleID int64) (externalReference, idempotencyKey string) {
	externalReference = strconv.FormatInt(ruleID, 10)
	return externalReference, automationRuleIdempotencyPrefix + externalReference
}

func validateAutomationReplayEvent(event *models.Event, rule *models.AutomationRule, profile *models.ResponseProfile) error {
	if err := validateAutomationReplayOwnership(event, rule, nil); err != nil {
		return err
	}
	if profile == nil || event == nil {
		return nil
	}
	if event.Mode != profile.Mode ||
		event.Strategy != profile.Strategy ||
		event.Level != profile.Level ||
		event.Priority != profile.Priority ||
		!sameInt32Ptr(event.CurtailBatchSize, profile.CurtailBatchSize) ||
		event.CurtailBatchIntervalSec != profile.CurtailBatchIntervalSec ||
		event.RestoreBatchSize != profile.RestoreBatchSize ||
		event.RestoreBatchIntervalSec != profile.RestoreBatchIntervalSec ||
		event.FanOffDelaySec != profile.FanOffDelaySec ||
		event.FanRestoreDelaySec != profile.FanRestoreDelaySec ||
		!slices.Equal(event.FacilityFanDeviceIDs, profile.FacilityFanDeviceIDs) {
		return fleeterror.NewFailedPreconditionError(
			"automation idempotency replay resolved to an event that no longer matches the automation rule profile",
		)
	}
	return nil
}

func sameInt32Ptr(a, b *int32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func validateAutomationReplayOwnership(event *models.Event, rule *models.AutomationRule, err error) error {
	if err != nil || event == nil {
		return err
	}
	externalReference, idempotencyKey := automationRuleEventReference(rule.ID)
	if event.OrgID != rule.OrgID ||
		event.SourceActorType != models.SourceActorAutomation ||
		event.ExternalSource == nil || *event.ExternalSource != automationExternalSource ||
		event.ExternalReference == nil || *event.ExternalReference != externalReference ||
		event.IdempotencyKey == nil || *event.IdempotencyKey != idempotencyKey ||
		event.SourceActorID == nil || *event.SourceActorID != externalReference {
		return fleeterror.NewFailedPreconditionError(
			"automation idempotency replay resolved to an event not owned by this automation rule",
		)
	}
	return nil
}

func stringPtr(s string) *string {
	return &s
}
