package curtailment

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// controlBounds uses pointers for patchable controls: nil means the caller is
// not validating that field. A present curtail interval also validates its
// relationship with the nullable curtail batch size.
type controlBounds struct {
	// Profiles historically describe fan upper bounds as a range.
	ProfileFanDelayErrors   bool
	CurtailBatchSize        *int32
	CurtailBatchIntervalSec *int32
	RestoreBatchSize        *int32
	RestoreBatchIntervalSec *int32
	FanOffDelaySec          *int32
	FanRestoreDelaySec      *int32
	MinCurtailedDurationSec *int32
	MaxDurationSeconds      *int32
}

func validateControlBounds(c controlBounds) error {
	if c.CurtailBatchSize != nil {
		if *c.CurtailBatchSize <= 0 {
			return fleeterror.NewInvalidArgumentErrorf("curtail_batch_size must be > 0 when set, got %d", *c.CurtailBatchSize)
		}
		if *c.CurtailBatchSize > RestoreBatchSizeMax {
			return fleeterror.NewInvalidArgumentErrorf("curtail_batch_size must be <= %d, got %d", RestoreBatchSizeMax, *c.CurtailBatchSize)
		}
	}
	if c.CurtailBatchIntervalSec != nil && c.CurtailBatchSize == nil && *c.CurtailBatchIntervalSec > 0 {
		return fleeterror.NewInvalidArgumentError("curtail_batch_interval_sec must be 0 when curtail_batch_size is unset")
	}
	fields := []struct {
		name  string
		value *int32
		max   int32
	}{
		{"restore_batch_size", c.RestoreBatchSize, RestoreBatchSizeMax},
		{"curtail_batch_interval_sec", c.CurtailBatchIntervalSec, batchIntervalUpperBoundSec},
		{"restore_batch_interval_sec", c.RestoreBatchIntervalSec, batchIntervalUpperBoundSec},
		{"fan_off_delay_sec", c.FanOffDelaySec, facilityFanDelayUpperBoundSec},
		{"fan_restore_delay_sec", c.FanRestoreDelaySec, facilityFanDelayUpperBoundSec},
		{"min_curtailed_duration_sec", c.MinCurtailedDurationSec, maxFiniteDurationSeconds},
	}
	for _, f := range fields {
		if f.value == nil {
			continue
		}
		if *f.value < 0 {
			return fleeterror.NewInvalidArgumentErrorf("%s must be >= 0, got %d", f.name, *f.value)
		}
		if f.max > 0 && *f.value > f.max {
			if c.ProfileFanDelayErrors && (f.name == "fan_off_delay_sec" || f.name == "fan_restore_delay_sec") {
				return fleeterror.NewInvalidArgumentErrorf("%s must be between 0 and %d, got %d", f.name, f.max, *f.value)
			}
			return fleeterror.NewInvalidArgumentErrorf("%s must be <= %d, got %d", f.name, f.max, *f.value)
		}
	}
	if c.MaxDurationSeconds != nil {
		if err := validateFiniteDuration("max_duration_seconds", *c.MaxDurationSeconds); err != nil {
			return err
		}
		if c.MinCurtailedDurationSec != nil && *c.MinCurtailedDurationSec > *c.MaxDurationSeconds {
			return fleeterror.NewInvalidArgumentError("min_curtailed_duration_sec must be <= max_duration_seconds")
		}
	}
	return nil
}

func validateFiniteDuration(field string, value int32) error {
	if value <= 0 {
		return fleeterror.NewInvalidArgumentErrorf("%s must be > 0, got %d", field, value)
	}
	if value > maxFiniteDurationSeconds {
		return fleeterror.NewInvalidArgumentErrorf("%s must be <= %d, got %d", field, maxFiniteDurationSeconds, value)
	}
	return nil
}

func validateReason(reason, emptyMessage string) error {
	if strings.TrimSpace(reason) == "" {
		return fleeterror.NewInvalidArgumentError(emptyMessage)
	}
	if n := utf8.RuneCountInString(reason); n > startTextFieldMaxLen {
		return fleeterror.NewInvalidArgumentErrorf("reason must be at most %d characters, got %d", startTextFieldMaxLen, n)
	}
	return nil
}

type adminControls struct {
	FullFleetProfile            bool
	CandidateMinPowerWOverride  *int32
	ForceIncludeMaintenance     bool
	ForceIncludeAllPairedMiners bool
	CurtailBatchIntervalSec     int32
	RestoreBatchIntervalSec     int32
}

// adminControlsRequirement is the common policy for profiles, Start, Update,
// and dispatch authorization. Callers choose only the controls they are changing;
// FULL_FLEET itself is admin-only for saved profiles, but not manual starts.
func adminControlsRequirement(c adminControls) string {
	switch {
	case c.FullFleetProfile:
		return "only admins can save response profiles with admin-only controls"
	case c.CandidateMinPowerWOverride != nil:
		return "only admins can set candidate_min_power_w_override"
	case c.ForceIncludeMaintenance:
		return "only admins can set force_include_maintenance"
	case c.ForceIncludeAllPairedMiners:
		return "only admins can set force_include_all_paired_miners"
	case c.CurtailBatchIntervalSec > nonAdminBatchIntervalMax:
		return fmt.Sprintf("only admins can set curtail_batch_interval_sec above %d", nonAdminBatchIntervalMax)
	case c.RestoreBatchIntervalSec > nonAdminBatchIntervalMax:
		return fmt.Sprintf("only admins can set restore_batch_interval_sec above %d", nonAdminBatchIntervalMax)
	default:
		return ""
	}
}

func validateAdminControlAccess(c adminControls, allowed bool) error {
	if !allowed {
		if requirement := adminControlsRequirement(c); requirement != "" {
			return fleeterror.NewForbiddenError(requirement)
		}
	}
	return nil
}

func startAdminControls(req StartRequest) adminControls {
	return adminControls{
		CandidateMinPowerWOverride: req.CandidateMinPowerWOverride,
		ForceIncludeMaintenance:    req.ForceIncludeMaintenance, ForceIncludeAllPairedMiners: req.ForceIncludeAllPairedMiners,
		CurtailBatchIntervalSec: req.CurtailBatchIntervalSec, RestoreBatchIntervalSec: req.RestoreBatchIntervalSec,
	}
}

func responseProfileAdminControls(profile models.ResponseProfile) adminControls {
	return adminControls{
		FullFleetProfile:        profile.Mode == models.ModeFullFleet,
		ForceIncludeMaintenance: profile.ForceIncludeMaintenance, ForceIncludeAllPairedMiners: profile.ForceIncludeAllPairedMiners,
		CurtailBatchIntervalSec: profile.CurtailBatchIntervalSec, RestoreBatchIntervalSec: profile.RestoreBatchIntervalSec,
	}
}

// ExecutionFence identifies the saved configuration an execution must recheck.
// Store inserts carry their creator user even without an automation fence;
// Recurtail accepts a service user only as part of a complete automation fence.
type ExecutionFence struct {
	ResponseProfileID       int64
	ResponseProfileRevision uuid.UUID
	AutomationRuleID        int64
	AutomationMQTTSourceID  int64
	AutomationServiceUserID int64
	SourceActorType         models.SourceActorType
}

type ExecutionFenceUse uint8

const (
	ExecutionFenceStart ExecutionFenceUse = iota
	ExecutionFenceRecurtail
	ExecutionFenceStore
)

func ValidateExecutionFence(f ExecutionFence, use ExecutionFenceUse) error {
	if use == ExecutionFenceStore && f.ResponseProfileID < 0 {
		return fleeterror.NewInvalidArgumentError("response profile execution fence requires a non-negative ID")
	}
	if (f.ResponseProfileID > 0) != (f.ResponseProfileRevision != uuid.Nil) {
		switch use {
		case ExecutionFenceStart:
			return fleeterror.NewInvalidArgumentError("response_profile_id and expected_response_profile_revision must be set together")
		case ExecutionFenceRecurtail:
			return fleeterror.NewInvalidArgumentError("response_profile_id and response_profile_revision must be set together")
		case ExecutionFenceStore:
			return fleeterror.NewInvalidArgumentError("response profile execution fence requires both profile ID and revision")
		default:
			return fleeterror.NewInvalidArgumentError("unsupported execution fence use")
		}
	}
	if f.ResponseProfileID < 0 {
		return fleeterror.NewInvalidArgumentError("response_profile_id must be non-negative")
	}
	if use == ExecutionFenceStart && (f.AutomationRuleID > 0) != (f.AutomationMQTTSourceID > 0) {
		return fleeterror.NewInvalidArgumentError("automation_rule_id and automation_mqtt_source_id must be set together")
	}
	if f.AutomationRuleID < 0 || f.AutomationMQTTSourceID < 0 || (use != ExecutionFenceStart && f.AutomationServiceUserID < 0) {
		return fleeterror.NewInvalidArgumentError("automation execution fence IDs must be non-negative")
	}
	if use == ExecutionFenceStart {
		if f.AutomationRuleID > 0 && (f.SourceActorType != models.SourceActorAutomation || f.ResponseProfileID == 0) {
			return fleeterror.NewInvalidArgumentError("automation execution fence requires an automation actor and response profile")
		}
		return nil
	}
	if use == ExecutionFenceStore && f.AutomationRuleID == 0 {
		if f.AutomationMQTTSourceID > 0 {
			return fleeterror.NewInvalidArgumentError("automation execution fence requires a rule ID with the MQTT source ID")
		}
		return nil
	}
	if (f.AutomationRuleID > 0) != (f.AutomationMQTTSourceID > 0) || (f.AutomationRuleID > 0) != (f.AutomationServiceUserID > 0) {
		return fleeterror.NewInvalidArgumentError("automation execution fence requires rule, MQTT source, and service user IDs")
	}
	if f.AutomationRuleID > 0 && (f.ResponseProfileID <= 0 || f.ResponseProfileRevision == uuid.Nil) {
		return fleeterror.NewInvalidArgumentError("automation execution fence requires a response profile ID and revision")
	}
	return nil
}
