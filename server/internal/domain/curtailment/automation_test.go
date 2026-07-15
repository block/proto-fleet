package curtailment

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestAutomationService_CreateValidatesSourceAndProfile(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.sources.configs[h.source.ID] = h.source
	h.profiles.profiles = []*models.ResponseProfile{h.profile}

	created, err := h.automation.Create(t.Context(), SaveAutomationRuleRequest{
		Rule: models.AutomationRule{
			OrgID:             h.orgID,
			RuleName:          "  MaestroOS curtailment  ",
			MQTTSourceID:      h.source.ID,
			ResponseProfileID: h.profile.ID,
		},
		CanUseAdminControls: true,
	})

	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "MaestroOS curtailment", created.RuleName)
	assert.Equal(t, models.AutomationTriggerTypeMQTT, created.TriggerType)
	assert.Equal(t, h.source.ID, created.MQTTSourceID)
	assert.Equal(t, h.profile.ID, created.ResponseProfileID)
	require.NotNil(t, h.rules.created)
	assert.Equal(t, "MaestroOS curtailment", h.rules.created.RuleName)
}

func TestAutomationService_CreateRejectsCrossOrgSource(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.sources.configs[h.source.ID] = mqttingest.SourceConfig{
		ID:             h.source.ID,
		OrganizationID: h.orgID + 1,
		SourceName:     h.source.SourceName,
	}
	h.profiles.profiles = []*models.ResponseProfile{h.profile}

	_, err := h.automation.Create(t.Context(), SaveAutomationRuleRequest{
		Rule: models.AutomationRule{
			OrgID:             h.orgID,
			RuleName:          "MaestroOS curtailment",
			MQTTSourceID:      h.source.ID,
			ResponseProfileID: h.profile.ID,
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
	assert.Contains(t, err.Error(), "MaestroOS source not found")
	assert.Equal(t, 0, h.rules.createCalls)
}

func TestAutomationService_CreateRejectsUnsupportedTriggerType(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.sources.configs[h.source.ID] = h.source
	h.profiles.profiles = []*models.ResponseProfile{h.profile}

	_, err := h.automation.Create(t.Context(), SaveAutomationRuleRequest{
		Rule: models.AutomationRule{
			OrgID:             h.orgID,
			RuleName:          "MaestroOS curtailment",
			TriggerType:       models.AutomationTriggerType("marketPriceAbove"),
			MQTTSourceID:      h.source.ID,
			ResponseProfileID: h.profile.ID,
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), `trigger_type "marketPriceAbove" is not supported`)
	assert.Contains(t, err.Error(), "only MQTT (MaestroOS source) is supported")
	assert.Equal(t, 0, h.rules.createCalls)
}

func TestAutomationService_CreateRejectsAdminOnlyProfileWithoutAdminControls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*models.ResponseProfile)
	}{
		{
			name: "full fleet automation",
			mutate: func(profile *models.ResponseProfile) {
				profile.Mode = models.ModeFullFleet
			},
		},
		{
			name: "force maintenance",
			mutate: func(profile *models.ResponseProfile) {
				profile.IncludeMaintenance = true
				profile.ForceIncludeMaintenance = true
			},
		},
		{
			name: "slow curtail batch interval",
			mutate: func(profile *models.ResponseProfile) {
				profile.CurtailBatchIntervalSec = nonAdminRestoreBatchIntervalMax + 1
			},
		},
		{
			name: "slow restore batch interval",
			mutate: func(profile *models.ResponseProfile) {
				profile.RestoreBatchIntervalSec = nonAdminRestoreBatchIntervalMax + 1
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newAutomationHarness(t)
			h.profile.Mode = models.ModeFixedKw
			tc.mutate(h.profile)

			_, err := h.automation.Create(t.Context(), SaveAutomationRuleRequest{
				Rule: models.AutomationRule{
					OrgID:             h.orgID,
					RuleName:          "MaestroOS curtailment",
					MQTTSourceID:      h.source.ID,
					ResponseProfileID: h.profile.ID,
				},
			})

			require.Error(t, err)
			assert.True(t, fleeterror.IsForbiddenError(err))
			assert.Equal(t, 0, h.rules.createCalls)
		})
	}
}

func TestAutomationService_CreateAllowsAdminOnlyProfileWithAdminControls(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.profile.IncludeMaintenance = true
	h.profile.ForceIncludeMaintenance = true

	created, err := h.automation.Create(t.Context(), SaveAutomationRuleRequest{
		Rule: models.AutomationRule{
			OrgID:             h.orgID,
			RuleName:          "MaestroOS curtailment",
			MQTTSourceID:      h.source.ID,
			ResponseProfileID: h.profile.ID,
		},
		CanUseAdminControls: true,
	})

	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, 1, h.rules.createCalls)
}

func TestAutomationService_UpdateRejectsAdminOnlyProfileWithoutAdminControls(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.profile.IncludeMaintenance = true
	h.profile.ForceIncludeMaintenance = true

	_, err := h.automation.Update(t.Context(), SaveAutomationRuleRequest{
		Rule: models.AutomationRule{
			ID:                h.rule.ID,
			OrgID:             h.orgID,
			RuleName:          "Renamed rule",
			MQTTSourceID:      h.source.ID,
			ResponseProfileID: h.profile.ID,
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))
	assert.Equal(t, "MaestroOS curtailment", h.rule.RuleName)
}

func TestAutomationService_UpdateRejectsWhenRuleOwnsActiveEvent(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedAutomationEvent(models.EventStateActive)

	_, err := h.automation.Update(t.Context(), SaveAutomationRuleRequest{
		Rule: models.AutomationRule{
			ID:                h.rule.ID,
			OrgID:             h.orgID,
			RuleName:          "Renamed rule",
			MQTTSourceID:      h.source.ID,
			ResponseProfileID: h.profile.ID,
		},
		CanUseAdminControls: true,
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Equal(t, "MaestroOS curtailment", h.rule.RuleName)
	assert.Equal(t, 0, h.rules.clearActiveCalls)
}

func TestAutomationService_SetEnabledRejectsDisableWhenRuleOwnsActiveEvent(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedAutomationEvent(models.EventStateRestoring)

	_, err := h.automation.SetEnabled(t.Context(), h.orgID, h.rule.ID, false, false)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.True(t, h.rule.Enabled)
	assert.Equal(t, 0, h.rules.clearActiveCalls)
}

func TestAutomationService_SetEnabledRejectsAdminOnlyProfileWithoutAdminControls(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.rule.Enabled = false
	h.profile.IncludeMaintenance = true
	h.profile.ForceIncludeMaintenance = true

	_, err := h.automation.SetEnabled(t.Context(), h.orgID, h.rule.ID, true, false)

	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))
	assert.False(t, h.rule.Enabled)
}

func TestAutomationService_DeleteRejectsWhenRuleOwnsActiveEvent(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedAutomationEvent(models.EventStateActive)

	err := h.automation.Delete(t.Context(), h.orgID, h.rule.ID)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Len(t, h.rules.rules, 1)
	assert.Equal(t, 0, h.rules.clearActiveCalls)
}

func TestAutomationService_SetEnabledClearsTerminalActiveEventBeforeDisable(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedAutomationEvent(models.EventStateCompleted)

	updated, err := h.automation.SetEnabled(t.Context(), h.orgID, h.rule.ID, false, false)

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.False(t, updated.Enabled)
	assert.Nil(t, updated.ActiveEventUUID)
	assert.Equal(t, 1, h.rules.clearActiveCalls)
}

func TestAutomationService_HandleMQTTSignal_OffStartsCurtailmentFromResponseProfile(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()

	receivedAt := time.Date(2026, 6, 11, 20, 0, 0, 0, time.UTC)
	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source:     h.source,
		Target:     mqttingest.TargetOff,
		ReceivedAt: receivedAt,
	})

	require.NoError(t, err)
	assert.Equal(t, []models.AutomationSignal{models.AutomationSignalOff}, h.rules.recordedSignals)
	assert.Equal(t, 1, h.curtailments.insertEventCalls)
	assert.Equal(t, models.ScopeTypeSite, h.curtailments.lastInsertEvent.ScopeType)
	require.NotNil(t, h.curtailments.lastInsertEvent.CurtailBatchSize)
	assert.Equal(t, int32(25), *h.curtailments.lastInsertEvent.CurtailBatchSize)
	assert.Equal(t, int32(15), h.curtailments.lastInsertEvent.CurtailBatchIntervalSec)
	assert.Equal(t, int32(50), h.curtailments.lastInsertEvent.RestoreBatchSize)
	assert.Equal(t, int32(5), h.curtailments.lastInsertEvent.RestoreBatchIntervalSec)
	expectedReason := `Automation "MaestroOS curtailment" from MaestroOS source "` + h.source.SourceName + `"`
	assert.Equal(
		t,
		expectedReason,
		h.curtailments.lastInsertEvent.Reason,
	)
	assert.Equal(t, models.SourceActorAutomation, h.curtailments.lastInsertEvent.SourceActorType)
	assert.True(t, h.curtailments.lastInsertEvent.AllowUnbounded)
	assert.Nil(t, h.curtailments.lastInsertEvent.MaxDurationSeconds)
	assert.Equal(t, h.source.ServiceUserID, h.curtailments.lastInsertEvent.CreatedByUserID)
	assert.Equal(t, automationExternalSource, *h.curtailments.lastInsertEvent.ExternalSource)
	assert.Equal(t, "9001", *h.curtailments.lastInsertEvent.ExternalReference)
	assert.Equal(t, "curtailment_automation_rule:9001", *h.curtailments.lastInsertEvent.IdempotencyKey)
	assert.Equal(t, 1, h.rules.setActiveCalls)
	require.NotNil(t, h.rule.ActiveEventUUID)
	assert.Equal(t, *h.rule.ActiveEventUUID, h.rules.lastActiveEvent)
	assert.Equal(t, receivedAt, h.rules.lastActiveAt)
}

func TestAutomationService_HandleMQTTSignal_OffRejectsFacilityFansUntilSequencingIsAvailable(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	h.profile.FacilityFanDeviceIDs = []int64{31}

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOff,
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "cannot execute until fan sequencing is available")
	assert.Equal(t, 0, h.curtailments.insertEventCalls)
	require.Len(t, h.rules.executionErrors, 1)
	assert.Contains(t, h.rules.executionErrors[0], "cannot execute until fan sequencing is available")
}

func TestAutomationService_HandleMQTTSignal_OffBypassesResponseProfileCooldown(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	h.profile.PostEventCooldownSec = 600
	h.curtailments.cooldownDevicesByOrg[h.orgID] = []string{"miner-a", "miner-b"}

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOff,
	})

	require.NoError(t, err)
	assert.Zero(t, h.curtailments.cooldownCalls)
	assert.Equal(t, 1, h.curtailments.insertEventCalls)
	assert.Equal(t, models.PriorityNormal, h.curtailments.lastInsertEvent.Priority)
	assert.Equal(t, models.ModeFullFleet, h.curtailments.lastInsertEvent.Mode)
	assert.Equal(t, models.LoopTypeClosed, h.curtailments.lastInsertEvent.LoopType)
	assert.Empty(t, h.curtailments.lastInsertTargets,
		"closed-loop full_fleet claims per-miner targets at dispatch time")
}

// TestAutomationService_HandleMQTTSignal_OffForwardsAllPairedPolicyFlag pins
// the automation execution plumbing: a response profile with the all-paired
// policy flag must stamp force_include_all_paired_miners on the persisted
// event and claim its policy targets at insert (the profile-level admin gate
// does not protect against a broken pass-through here).
func TestAutomationService_HandleMQTTSignal_OffForwardsAllPairedPolicyFlag(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	h.profile.ForceIncludeAllPairedMiners = true

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOff,
	})

	require.NoError(t, err)
	assert.Equal(t, 1, h.curtailments.insertEventCalls)
	assert.True(t, h.curtailments.lastInsertEvent.ForceIncludeAllPairedMiners,
		"automation execution must forward the profile's all-paired flag onto the persisted event")
	assert.Len(t, h.curtailments.lastInsertTargets, 2,
		"all-paired automation starts persist policy target rows at insert instead of deferring to dispatch-time claims")
}

func TestAutomationService_HandleMQTTSignal_RepeatedOffNoopsWhenEventIsActive(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	activeEventUUID := uuid.New()
	h.rule.ActiveEventUUID = &activeEventUUID
	h.curtailments.eventsByUUID[activeEventUUID] = &models.Event{
		ID:        77,
		EventUUID: activeEventUUID,
		OrgID:     h.orgID,
		State:     models.EventStateActive,
	}

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOff,
	})

	require.NoError(t, err)
	assert.Equal(t, 0, h.curtailments.insertEventCalls)
	assert.Equal(t, 0, h.curtailments.beginRecurtailCalls)
	assert.Equal(t, 0, h.rules.setActiveCalls)
}

func TestAutomationService_HandleMQTTSignal_OffRecapturesAfterCancelledEvent(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	activeEventUUID := uuid.New()
	h.rule.ActiveEventUUID = &activeEventUUID
	h.curtailments.eventsByUUID[activeEventUUID] = &models.Event{
		ID:        77,
		EventUUID: activeEventUUID,
		OrgID:     h.orgID,
		State:     models.EventStateCancelled,
	}

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOff,
	})

	require.NoError(t, err)
	assert.Equal(t, []models.AutomationSignal{models.AutomationSignalOff}, h.rules.recordedSignals)
	assert.Equal(t, 1, h.curtailments.insertEventCalls)
	assert.Equal(t, 0, h.curtailments.beginRecurtailCalls)
	assert.Equal(t, 1, h.rules.setActiveCalls)
	require.NotNil(t, h.rule.ActiveEventUUID)
	assert.NotEqual(t, activeEventUUID, *h.rule.ActiveEventUUID)
	assert.Equal(t, *h.rule.ActiveEventUUID, h.rules.lastActiveEvent)
}

func TestAutomationService_HandleMQTTSignal_OffDoesNotRecordActiveEventAfterRuleDisabled(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	activeEventUUID := uuid.New()
	h.rule.ActiveEventUUID = &activeEventUUID
	h.curtailments.eventsByUUID[activeEventUUID] = &models.Event{
		ID:        77,
		EventUUID: activeEventUUID,
		OrgID:     h.orgID,
		State:     models.EventStateCancelled,
	}
	staleRule := *h.rule
	h.rule.Enabled = false

	err := h.automation.handleRuleSignal(t.Context(), &staleRule, mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOff,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
	assert.Equal(t, []models.AutomationSignal{models.AutomationSignalOff}, h.rules.recordedSignals)
	assert.Equal(t, 1, h.curtailments.insertEventCalls)
	assert.Equal(t, 1, h.rules.setActiveCalls)
	assert.Equal(t, 1, h.curtailments.forceReleaseCalls)
	assert.Equal(t, h.curtailments.lastInsertEvent.EventUUID, h.curtailments.lastForceReleaseUUID)
	require.NotNil(t, h.rule.ActiveEventUUID)
	assert.Equal(t, activeEventUUID, *h.rule.ActiveEventUUID)
}

func TestAutomationService_HandleMQTTSignal_OffDoesNotReleaseOnTransientActiveEventRecordFailure(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	activeEventUUID := uuid.New()
	h.rule.ActiveEventUUID = &activeEventUUID
	h.curtailments.eventsByUUID[activeEventUUID] = &models.Event{
		ID:        77,
		EventUUID: activeEventUUID,
		OrgID:     h.orgID,
		State:     models.EventStateCancelled,
	}
	h.rules.setActiveErr = assert.AnError

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOff,
	})

	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, []models.AutomationSignal{models.AutomationSignalOff}, h.rules.recordedSignals)
	assert.Equal(t, 1, h.curtailments.insertEventCalls)
	assert.Equal(t, 1, h.rules.setActiveCalls)
	assert.Equal(t, 0, h.curtailments.forceReleaseCalls)
	require.NotNil(t, h.rule.ActiveEventUUID)
	assert.Equal(t, activeEventUUID, *h.rule.ActiveEventUUID)
}

func TestAutomationService_HandleMQTTSignal_CoalescesRecentRepeatedOff(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	activeEventUUID := h.seedAutomationEvent(models.EventStateActive)
	lastSignal := models.AutomationSignalOff
	lastSignalAt := time.Date(2026, 6, 11, 20, 0, 0, 0, time.UTC)
	h.rule.ActiveEventUUID = &activeEventUUID
	h.rule.LastSignal = &lastSignal
	h.rule.LastSignalAt = &lastSignalAt

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source:     h.source,
		Direction:  mqttingest.EdgeReassertOff,
		Target:     mqttingest.TargetOff,
		ReceivedAt: lastSignalAt.Add(10 * time.Second),
	})

	require.NoError(t, err)
	assert.Empty(t, h.rules.recordedSignals)
	assert.Equal(t, 0, h.curtailments.insertEventCalls)
	assert.Equal(t, 0, h.curtailments.beginRecurtailCalls)
	assert.Equal(t, 0, h.rules.setActiveCalls)
}

func TestAutomationService_HandleMQTTSignal_RepeatedOffRecurtailsRecentRestoringEvent(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	activeEventUUID := h.seedAutomationEvent(models.EventStateRestoring)
	lastSignal := models.AutomationSignalOff
	lastSignalAt := time.Date(2026, 6, 11, 20, 0, 0, 0, time.UTC)
	h.rule.ActiveEventUUID = &activeEventUUID
	h.rule.LastSignal = &lastSignal
	h.rule.LastSignalAt = &lastSignalAt

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source:     h.source,
		Direction:  mqttingest.EdgeReassertOff,
		Target:     mqttingest.TargetOff,
		ReceivedAt: lastSignalAt.Add(10 * time.Second),
	})

	require.NoError(t, err)
	assert.Equal(t, []models.AutomationSignal{models.AutomationSignalOff}, h.rules.recordedSignals)
	assert.Equal(t, 1, h.curtailments.beginRecurtailCalls)
	assert.Equal(t, activeEventUUID, h.curtailments.beginRecurtailLastEventID)
	assert.Equal(t, 1, h.rules.setActiveCalls)
	assert.Equal(t, activeEventUUID, h.rules.lastActiveEvent)
}

func TestAutomationService_HandleMQTTSignal_RepeatedOffDoesNotRecurtailAfterMaxDuration(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	activeEventUUID := h.seedAutomationEvent(models.EventStateRestoring)
	lastSignal := models.AutomationSignalOff
	lastSignalAt := time.Date(2026, 6, 11, 20, 0, 0, 0, time.UTC)
	startedAt := lastSignalAt.Add(-time.Hour)
	maxDurationSeconds := int32(300)
	h.rule.ActiveEventUUID = &activeEventUUID
	h.rule.LastSignal = &lastSignal
	h.rule.LastSignalAt = &lastSignalAt
	h.curtailments.eventsByUUID[activeEventUUID].StartedAt = &startedAt
	h.curtailments.eventsByUUID[activeEventUUID].MaxDurationSeconds = &maxDurationSeconds

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source:     h.source,
		Direction:  mqttingest.EdgeReassertOff,
		Target:     mqttingest.TargetOff,
		ReceivedAt: lastSignalAt.Add(10 * time.Second),
	})

	require.NoError(t, err)
	assert.Equal(t, []models.AutomationSignal{models.AutomationSignalOff}, h.rules.recordedSignals)
	assert.Equal(t, 0, h.curtailments.beginRecurtailCalls)
	assert.Equal(t, 0, h.rules.setActiveCalls)
}

func TestAutomationService_HandleMQTTSignal_ReplayedOffDoesNotBypassMaxDuration(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	activeEventUUID := h.seedAutomationEvent(models.EventStateRestoring)
	now := time.Date(2026, 6, 11, 21, 0, 0, 0, time.UTC)
	receivedAt := now.Add(-8 * time.Minute)
	lastSignal := models.AutomationSignalOff
	lastSignalAt := receivedAt.Add(-time.Minute)
	startedAt := now.Add(-10 * time.Minute)
	maxDurationSeconds := int32(300)
	h.rule.ActiveEventUUID = &activeEventUUID
	h.rule.LastSignal = &lastSignal
	h.rule.LastSignalAt = &lastSignalAt
	h.curtailments.eventsByUUID[activeEventUUID].StartedAt = &startedAt
	h.curtailments.eventsByUUID[activeEventUUID].MaxDurationSeconds = &maxDurationSeconds

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source:     h.source,
		Direction:  mqttingest.EdgeReassertOff,
		Target:     mqttingest.TargetOff,
		ReceivedAt: receivedAt,
	})

	require.NoError(t, err)
	assert.Equal(t, []models.AutomationSignal{models.AutomationSignalOff}, h.rules.recordedSignals)
	assert.Equal(t, receivedAt, *h.rule.LastSignalAt)
	assert.Equal(t, 0, h.curtailments.beginRecurtailCalls)
	assert.Equal(t, 0, h.rules.setActiveCalls)
}

func TestAutomationService_HandleMQTTSignal_OnStartsRestoreAndKeepsActiveEventForRecurtail(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	activeEventUUID := uuid.New()
	h.rule.ActiveEventUUID = &activeEventUUID
	h.curtailments.eventsByUUID[activeEventUUID] = &models.Event{
		ID:        77,
		EventUUID: activeEventUUID,
		OrgID:     h.orgID,
		State:     models.EventStateActive,
	}

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOn,
	})

	require.NoError(t, err)
	assert.Equal(t, []models.AutomationSignal{models.AutomationSignalOn}, h.rules.recordedSignals)
	assert.Equal(t, 1, h.curtailments.beginRestoreCalls)
	assert.Equal(t, activeEventUUID, h.curtailments.beginRestoreLastEventID)
	assert.Equal(t, 1, h.rules.restoreStartedCalls)
	require.NotNil(t, h.rule.LastRestoredAt)
	assert.Equal(t, *h.rule.LastRestoredAt, h.rules.lastRestoreStartedAt)
	assert.Equal(t, 0, h.rules.clearActiveCalls)
	require.NotNil(t, h.rule.ActiveEventUUID)
	assert.Equal(t, activeEventUUID, *h.rule.ActiveEventUUID)
}

func TestAutomationService_HandleMQTTSignal_OnRestoresEventWhenActiveStateWasNotRecorded(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	h.rules.setActiveErr = errors.New("automation state unavailable")

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOff,
	})

	require.Error(t, err)
	assert.Nil(t, h.rule.ActiveEventUUID)
	require.Equal(t, 1, h.curtailments.insertEventCalls)
	eventUUID := h.curtailments.lastInsertEvent.EventUUID
	event := &models.Event{
		ID:        77,
		EventUUID: eventUUID,
		OrgID:     h.orgID,
		State:     models.EventStateActive,
	}
	externalReference, idempotencyKey := automationRuleEventReference(h.rule.ID)
	h.curtailments.eventsByUUID[eventUUID] = event
	h.curtailments.eventsByIdempotencyKey = map[string]*models.Event{idempotencyKey: event}
	h.curtailments.eventsByExternalRef = map[string]*models.Event{
		automationExternalSource + "|" + externalReference: event,
	}
	h.rules.setActiveErr = nil

	err = h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOn,
	})

	require.NoError(t, err)
	assert.Equal(t, 1, h.curtailments.beginRestoreCalls)
	assert.Equal(t, eventUUID, h.curtailments.beginRestoreLastEventID)
	assert.Equal(t, 1, h.rules.restoreStartedCalls)
	assert.Nil(t, h.rule.ActiveEventUUID)
	require.NotNil(t, h.rule.LastRestoredAt)
}

func TestAutomationService_HandleMQTTSignal_OffDuringRestoreRecurtailsSameEvent(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	activeEventUUID := uuid.New()
	h.rule.ActiveEventUUID = &activeEventUUID
	h.curtailments.eventsByUUID[activeEventUUID] = &models.Event{
		ID:        77,
		EventUUID: activeEventUUID,
		OrgID:     h.orgID,
		State:     models.EventStateRestoring,
	}

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOff,
	})

	require.NoError(t, err)
	assert.Equal(t, 0, h.curtailments.insertEventCalls)
	assert.Equal(t, 1, h.curtailments.beginRecurtailCalls)
	assert.Equal(t, activeEventUUID, h.curtailments.beginRecurtailLastEventID)
	assert.Equal(t, 1, h.rules.setActiveCalls)
	assert.Equal(t, activeEventUUID, h.rules.lastActiveEvent)
}

func TestAutomationService_HandleMQTTSignal_OnClearsTerminalActiveEvent(t *testing.T) {
	t.Parallel()

	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	activeEventUUID := uuid.New()
	h.rule.ActiveEventUUID = &activeEventUUID
	h.curtailments.eventsByUUID[activeEventUUID] = &models.Event{
		ID:        77,
		EventUUID: activeEventUUID,
		OrgID:     h.orgID,
		State:     models.EventStateCompleted,
	}

	err := h.automation.HandleMQTTSignal(t.Context(), mqttingest.SignalEdge{
		Source: h.source,
		Target: mqttingest.TargetOn,
	})

	require.NoError(t, err)
	assert.Equal(t, 0, h.curtailments.beginRestoreCalls)
	assert.Equal(t, 1, h.rules.clearActiveCalls)
	assert.Nil(t, h.rule.ActiveEventUUID)
}

type automationHarness struct {
	t            *testing.T
	orgID        int64
	source       mqttingest.SourceConfig
	profile      *models.ResponseProfile
	rule         *models.AutomationRule
	rules        *automationFakeStore
	sources      *automationSourceStore
	profiles     *responseProfileFakeStore
	curtailments *fakeStore
	automation   *AutomationService
}

func newAutomationHarness(t *testing.T) *automationHarness {
	t.Helper()

	const orgID = int64(42)
	siteID := int64(7)
	curtailBatchSize := int32(25)
	source := mqttingest.SourceConfig{
		ID:             7001,
		OrganizationID: orgID,
		ServiceUserID:  501,
		SourceName:     "Site Alpha MaestroOS",
		Topic:          "maestro/target",
	}
	profile := &models.ResponseProfile{
		ID:                      3001,
		OrgID:                   orgID,
		ProfileName:             "Standard shed",
		SiteID:                  &siteID,
		Mode:                    models.ModeFullFleet,
		Strategy:                models.StrategyLeastEfficientFirst,
		Level:                   models.LevelFull,
		Priority:                models.PriorityNormal,
		CurtailBatchSize:        &curtailBatchSize,
		CurtailBatchIntervalSec: 15,
		RestoreBatchSize:        50,
		RestoreBatchIntervalSec: 5,
	}
	rule := &models.AutomationRule{
		ID:                9001,
		OrgID:             orgID,
		RuleName:          "MaestroOS curtailment",
		TriggerType:       models.AutomationTriggerTypeMQTT,
		MQTTSourceID:      source.ID,
		MQTTSourceName:    source.SourceName,
		ResponseProfileID: profile.ID,
		Enabled:           true,
	}
	rules := newAutomationFakeStore(rule)
	sources := &automationSourceStore{configs: map[int64]mqttingest.SourceConfig{source.ID: source}}
	profiles := newResponseProfileFakeStore()
	profiles.profiles = []*models.ResponseProfile{profile}
	curtailments := newFakeStore()
	curtailmentSvc := NewService(curtailments)
	automation, err := NewAutomationService(AutomationServiceConfig{
		Store:       rules,
		Profiles:    NewResponseProfileService(profiles),
		SourceStore: sources,
		Curtailment: curtailmentSvc,
		Clock:       func() time.Time { return time.Date(2026, 6, 11, 21, 0, 0, 0, time.UTC) },
	})
	require.NoError(t, err)

	return &automationHarness{
		t:            t,
		orgID:        orgID,
		source:       source,
		profile:      profile,
		rule:         rule,
		rules:        rules,
		sources:      sources,
		profiles:     profiles,
		curtailments: curtailments,
		automation:   automation,
	}
}

func (h *automationHarness) seedAutomationEvent(state models.EventState) uuid.UUID {
	h.t.Helper()

	activeEventUUID := uuid.New()
	h.rule.ActiveEventUUID = &activeEventUUID
	h.curtailments.eventsByUUID[activeEventUUID] = &models.Event{
		ID:        77,
		EventUUID: activeEventUUID,
		OrgID:     h.orgID,
		State:     state,
	}
	return activeEventUUID
}

func (h *automationHarness) seedRunnableProfile() {
	h.t.Helper()

	h.curtailments.orgConfigByOrg[h.orgID] = defaultOrgConfig(h.orgID)
	h.curtailments.sitesByOrg[h.orgID] = map[int64]bool{*h.profile.SiteID: true}
	h.curtailments.candidatesBySite[h.orgID] = map[int64][]*models.Candidate{
		*h.profile.SiteID: {
			minerWithEff("miner-a", 3000, 100, 50),
			minerWithEff("miner-b", 3000, 100, 40),
		},
	}
}

type automationFakeStore struct {
	mu                   sync.Mutex
	nextID               int64
	rules                []*models.AutomationRule
	created              *models.AutomationRule
	createCalls          int
	recordedSignals      []models.AutomationSignal
	setActiveCalls       int
	lastActiveEvent      uuid.UUID
	lastActiveAt         time.Time
	setActiveErr         error
	restoreStartedCalls  int
	lastRestoreStartedAt time.Time
	clearActiveCalls     int
	lastClearedAt        time.Time
	executionErrors      []string
}

func newAutomationFakeStore(rules ...*models.AutomationRule) *automationFakeStore {
	return &automationFakeStore{
		nextID: 1,
		rules:  rules,
	}
}

func (f *automationFakeStore) ListAutomationRules(_ context.Context, orgID int64) ([]*models.AutomationRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*models.AutomationRule, 0, len(f.rules))
	for _, rule := range f.rules {
		if rule.OrgID == orgID {
			out = append(out, rule)
		}
	}
	return out, nil
}

func (f *automationFakeStore) GetAutomationRule(_ context.Context, orgID, ruleID int64) (*models.AutomationRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, rule := range f.rules {
		if rule.OrgID == orgID && rule.ID == ruleID {
			return rule, nil
		}
	}
	return nil, fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", ruleID)
}

func (f *automationFakeStore) ListEnabledAutomationRulesByMQTTSource(_ context.Context, mqttSourceID int64) ([]*models.AutomationRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*models.AutomationRule, 0, len(f.rules))
	for _, rule := range f.rules {
		if rule.Enabled && rule.TriggerType == models.AutomationTriggerTypeMQTT && rule.MQTTSourceID == mqttSourceID {
			out = append(out, rule)
		}
	}
	return out, nil
}

func (f *automationFakeStore) CreateAutomationRule(_ context.Context, rule models.AutomationRule) (*models.AutomationRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	if rule.ID == 0 {
		rule.ID = f.nextID
		f.nextID++
	}
	f.created = &rule
	f.rules = append(f.rules, &rule)
	return &rule, nil
}

func (f *automationFakeStore) UpdateAutomationRule(_ context.Context, rule models.AutomationRule) (*models.AutomationRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, existing := range f.rules {
		if existing.OrgID == rule.OrgID && existing.ID == rule.ID {
			f.rules[i] = &rule
			return &rule, nil
		}
	}
	return nil, fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", rule.ID)
}

func (f *automationFakeStore) SetAutomationRuleEnabled(_ context.Context, orgID, ruleID int64, enabled bool) (*models.AutomationRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, rule := range f.rules {
		if rule.OrgID == orgID && rule.ID == ruleID {
			rule.Enabled = enabled
			return rule, nil
		}
	}
	return nil, fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", ruleID)
}

func (f *automationFakeStore) DeleteAutomationRule(_ context.Context, orgID, ruleID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, rule := range f.rules {
		if rule.OrgID == orgID && rule.ID == ruleID {
			f.rules = append(f.rules[:i], f.rules[i+1:]...)
			return nil
		}
	}
	return fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", ruleID)
}

func (f *automationFakeStore) CountAutomationRulesByMQTTSource(_ context.Context, orgID, sourceID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var count int64
	for _, rule := range f.rules {
		if rule.OrgID == orgID && rule.MQTTSourceID == sourceID {
			count++
		}
	}
	return count, nil
}

func (f *automationFakeStore) RecordAutomationSignal(_ context.Context, ruleID int64, signal models.AutomationSignal, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordedSignals = append(f.recordedSignals, signal)
	for _, rule := range f.rules {
		if rule.ID == ruleID {
			value := signal
			rule.LastSignal = &value
			rule.LastSignalAt = &at
			return nil
		}
	}
	return fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", ruleID)
}

func (f *automationFakeStore) SetAutomationActiveEvent(_ context.Context, ruleID, mqttSourceID int64, eventUUID uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setActiveCalls++
	f.lastActiveEvent = eventUUID
	f.lastActiveAt = at
	if f.setActiveErr != nil {
		return f.setActiveErr
	}
	for _, rule := range f.rules {
		if rule.ID == ruleID {
			if !rule.Enabled || rule.MQTTSourceID != mqttSourceID {
				return fleeterror.NewFailedPreconditionErrorf(
					"curtailment automation rule %d is disabled or no longer bound to MQTT source %d", ruleID, mqttSourceID)
			}
			rule.ActiveEventUUID = &eventUUID
			rule.LastStartedAt = &at
			return nil
		}
	}
	return fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", ruleID)
}

func (f *automationFakeStore) ClearAutomationActiveEvent(_ context.Context, ruleID int64, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearActiveCalls++
	f.lastClearedAt = at
	for _, rule := range f.rules {
		if rule.ID == ruleID {
			rule.ActiveEventUUID = nil
			rule.LastRestoredAt = &at
			return nil
		}
	}
	return fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", ruleID)
}

func (f *automationFakeStore) RecordAutomationRestoreStarted(_ context.Context, ruleID int64, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restoreStartedCalls++
	f.lastRestoreStartedAt = at
	for _, rule := range f.rules {
		if rule.ID == ruleID {
			rule.LastRestoredAt = &at
			return nil
		}
	}
	return fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", ruleID)
}

func (f *automationFakeStore) RecordAutomationExecutionError(_ context.Context, ruleID int64, message string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executionErrors = append(f.executionErrors, message)
	for _, rule := range f.rules {
		if rule.ID == ruleID {
			rule.LastError = &message
			rule.LastErrorAt = &at
			return nil
		}
	}
	return fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", ruleID)
}

type automationSourceStore struct {
	configs map[int64]mqttingest.SourceConfig
}

func (s *automationSourceStore) ListSourceConfigsByOrg(_ context.Context, orgID int64) ([]mqttingest.SourceConfig, error) {
	out := make([]mqttingest.SourceConfig, 0, len(s.configs))
	for _, cfg := range s.configs {
		if cfg.OrganizationID == orgID {
			out = append(out, cfg)
		}
	}
	return out, nil
}

func (s *automationSourceStore) ListSourceStatesByOrg(context.Context, int64) ([]mqttingest.SourceState, error) {
	return nil, nil
}

func (s *automationSourceStore) GetSourceConfigByOrg(_ context.Context, orgID, sourceID int64) (mqttingest.SourceConfig, error) {
	cfg, ok := s.configs[sourceID]
	if !ok || cfg.OrganizationID != orgID {
		return mqttingest.SourceConfig{}, mqttingest.ErrSourceConfigNotFound
	}
	return cfg, nil
}

func (s *automationSourceStore) CreateSourceConfig(_ context.Context, source mqttingest.SourceConfig) (mqttingest.SourceConfig, error) {
	if s.configs == nil {
		s.configs = map[int64]mqttingest.SourceConfig{}
	}
	s.configs[source.ID] = source
	return source, nil
}

func (s *automationSourceStore) UpdateSourceConfig(_ context.Context, source mqttingest.SourceConfig) (mqttingest.SourceConfig, error) {
	if s.configs == nil {
		s.configs = map[int64]mqttingest.SourceConfig{}
	}
	s.configs[source.ID] = source
	return source, nil
}

func (s *automationSourceStore) SetSourceConfigEnabled(_ context.Context, orgID, sourceID int64, enabled bool) (mqttingest.SourceConfig, error) {
	cfg, err := s.GetSourceConfigByOrg(context.Background(), orgID, sourceID)
	if err != nil {
		return mqttingest.SourceConfig{}, err
	}
	cfg.Enabled = enabled
	s.configs[sourceID] = cfg
	return cfg, nil
}

func (s *automationSourceStore) DeleteDisabledSourceConfig(_ context.Context, orgID, sourceID int64) error {
	if _, err := s.GetSourceConfigByOrg(context.Background(), orgID, sourceID); err != nil {
		return err
	}
	delete(s.configs, sourceID)
	return nil
}

func (s *automationSourceStore) CountAutomationRulesByMQTTSource(context.Context, int64, int64) (int64, error) {
	return 0, nil
}
