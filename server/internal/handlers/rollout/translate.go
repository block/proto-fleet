package rollout

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

var (
	methodToProto = map[string]pb.RolloutMethod{
		rollout.MethodAllAtOnce:         pb.RolloutMethod_ROLLOUT_METHOD_ALL_AT_ONCE,
		rollout.MethodBatched:           pb.RolloutMethod_ROLLOUT_METHOD_BATCHED,
		rollout.MethodPilotThenContinue: pb.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE,
		rollout.MethodDelegated:         pb.RolloutMethod_ROLLOUT_METHOD_DELEGATED,
	}
	methodFromProto = map[pb.RolloutMethod]string{
		pb.RolloutMethod_ROLLOUT_METHOD_ALL_AT_ONCE:         rollout.MethodAllAtOnce,
		pb.RolloutMethod_ROLLOUT_METHOD_BATCHED:             rollout.MethodBatched,
		pb.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE: rollout.MethodPilotThenContinue,
		pb.RolloutMethod_ROLLOUT_METHOD_DELEGATED:           rollout.MethodDelegated,
	}
	orderToProto = map[string]pb.RolloutOrder{
		rollout.OrderLeastEfficientFirst: pb.RolloutOrder_ROLLOUT_ORDER_LEAST_EFFICIENT_FIRST,
		rollout.OrderRandom:              pb.RolloutOrder_ROLLOUT_ORDER_RANDOM,
	}
	orderFromProto = map[pb.RolloutOrder]string{
		pb.RolloutOrder_ROLLOUT_ORDER_LEAST_EFFICIENT_FIRST: rollout.OrderLeastEfficientFirst,
		pb.RolloutOrder_ROLLOUT_ORDER_RANDOM:                rollout.OrderRandom,
	}
	statusToProto = map[string]pb.RolloutStatus{
		rollout.StatusActive:                pb.RolloutStatus_ROLLOUT_STATUS_ACTIVE,
		rollout.StatusCompleted:             pb.RolloutStatus_ROLLOUT_STATUS_COMPLETED,
		rollout.StatusCompletedWithFailures: pb.RolloutStatus_ROLLOUT_STATUS_COMPLETED_WITH_FAILURES,
		rollout.StatusCanceled:              pb.RolloutStatus_ROLLOUT_STATUS_CANCELED,
	}
	statusFromProto = map[pb.RolloutStatus]string{
		pb.RolloutStatus_ROLLOUT_STATUS_ACTIVE:                  rollout.StatusActive,
		pb.RolloutStatus_ROLLOUT_STATUS_COMPLETED:               rollout.StatusCompleted,
		pb.RolloutStatus_ROLLOUT_STATUS_COMPLETED_WITH_FAILURES: rollout.StatusCompletedWithFailures,
		pb.RolloutStatus_ROLLOUT_STATUS_CANCELED:                rollout.StatusCanceled,
	}
	stateToProto = map[string]pb.RolloutState{
		rollout.StateInProgress:            pb.RolloutState_ROLLOUT_STATE_IN_PROGRESS,
		rollout.StateStabilizingTelemetry:  pb.RolloutState_ROLLOUT_STATE_STABILIZING_TELEMETRY,
		rollout.StatePausedAtPilotGate:     pb.RolloutState_ROLLOUT_STATE_PAUSED_AT_PILOT_GATE,
		rollout.StatePausedAtBatchReview:   pb.RolloutState_ROLLOUT_STATE_PAUSED_AT_BATCH_REVIEW,
		rollout.StatePaused:                pb.RolloutState_ROLLOUT_STATE_PAUSED,
		rollout.StateCompleted:             pb.RolloutState_ROLLOUT_STATE_COMPLETED,
		rollout.StateCompletedWithFailures: pb.RolloutState_ROLLOUT_STATE_COMPLETED_WITH_FAILURES,
		rollout.StateCanceled:              pb.RolloutState_ROLLOUT_STATE_CANCELED,
		rollout.StateWaitingForController:  pb.RolloutState_ROLLOUT_STATE_WAITING_FOR_CONTROLLER,
	}
	stageToProto = map[string]pb.RolloutStage{
		rollout.StageBatch:          pb.RolloutStage_ROLLOUT_STAGE_BATCH,
		rollout.StageAwaitingReview: pb.RolloutStage_ROLLOUT_STAGE_AWAITING_REVIEW,
		rollout.StageWaiting:        pb.RolloutStage_ROLLOUT_STAGE_WAITING,
		rollout.StageRest:           pb.RolloutStage_ROLLOUT_STAGE_REST,
	}
	cancelReasonToProto = map[string]pb.RolloutCancelReason{
		rollout.CancelReasonSuperseded:        pb.RolloutCancelReason_ROLLOUT_CANCEL_REASON_SUPERSEDED,
		rollout.CancelReasonCanceledRemaining: pb.RolloutCancelReason_ROLLOUT_CANCEL_REASON_CANCELED_REMAINING,
		rollout.CancelReasonRolledBack:        pb.RolloutCancelReason_ROLLOUT_CANCEL_REASON_ROLLED_BACK,
		rollout.CancelReasonCleared:           pb.RolloutCancelReason_ROLLOUT_CANCEL_REASON_CLEARED,
	}
	phaseToProto = map[string]pb.RolloutDevicePhase{
		rollout.PhaseQueued:     pb.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_QUEUED,
		rollout.PhaseInProgress: pb.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_IN_PROGRESS,
		rollout.PhaseRetrying:   pb.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_RETRYING,
		rollout.PhaseDone:       pb.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_DONE,
		rollout.PhaseFailed:     pb.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_FAILED,
		rollout.PhaseExcluded:   pb.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_EXCLUDED,
		rollout.PhaseSkipped:    pb.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_SKIPPED,
	}
	actorTypeToProto = map[string]pb.RolloutActorType{
		rollout.ActorTypeUser:   pb.RolloutActorType_ROLLOUT_ACTOR_TYPE_USER,
		rollout.ActorTypeAPIKey: pb.RolloutActorType_ROLLOUT_ACTOR_TYPE_API_KEY,
		rollout.ActorTypeSystem: pb.RolloutActorType_ROLLOUT_ACTOR_TYPE_SYSTEM,
	}
	resolutionToProto = map[string]pb.ReleaseChannelConflictResolution{
		rollout.ResolutionWinner:      pb.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
		rollout.ResolutionLoser:       pb.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_LOSER,
		rollout.ResolutionExcludedTie: pb.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_EXCLUDED_TIE,
	}
	reasonToProto = map[string]pb.RolloutErrorReason{
		rollout.ReasonStaleRevision:     pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_STALE_REVISION,
		rollout.ReasonStaleGeneration:   pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_STALE_GENERATION,
		rollout.ReasonNotActive:         pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_NOT_ACTIVE,
		rollout.ReasonNotAtGate:         pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_NOT_AT_GATE,
		rollout.ReasonNotDelegated:      pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_NOT_DELEGATED,
		rollout.ReasonPaused:            pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_PAUSED,
		rollout.ReasonOfflineBudgetFull: pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_OFFLINE_BUDGET_FULL,
		rollout.ReasonDeviceNotQueued:   pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_DEVICE_NOT_QUEUED,
		rollout.ReasonScopeOverlap:      pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_SCOPE_OVERLAP,
		rollout.ReasonArtifactMissing:   pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_ARTIFACT_MISSING,
		rollout.ReasonRolloutActive:     pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_ROLLOUT_ACTIVE,
		rollout.ReasonUpdatesInFlight:   pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_UPDATES_IN_FLIGHT,
		rollout.ReasonArtifactMismatch:  pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_ARTIFACT_MISMATCH,
	}
)

func scopeFromProto(s *pb.ReleaseChannelScope) rollout.Scope {
	if s == nil {
		return rollout.Scope{}
	}
	return rollout.Scope{
		SiteIDs:           s.SiteIds,
		BuildingIDs:       s.BuildingIds,
		RackIDs:           s.RackIds,
		GroupIDs:          s.GroupIds,
		DeviceIdentifiers: s.DeviceIdentifiers,
	}
}

func scopeToProto(s rollout.Scope) *pb.ReleaseChannelScope {
	return &pb.ReleaseChannelScope{
		SiteIds:           s.SiteIDs,
		BuildingIds:       s.BuildingIDs,
		RackIds:           s.RackIDs,
		GroupIds:          s.GroupIDs,
		DeviceIdentifiers: s.DeviceIdentifiers,
	}
}

// behaviorFromProto maps the wire behavior; unknown enum values fall through
// as empty strings, which the domain rejects.
func behaviorFromProto(b *pb.RolloutBehavior) rollout.Behavior {
	if b == nil {
		return rollout.Behavior{}
	}
	out := rollout.Behavior{
		Method:                    methodFromProto[b.Method],
		Order:                     orderFromProto[b.Order],
		BatchSize:                 b.BatchSize,
		PilotSize:                 b.PilotSize,
		WaitBetweenBatchesSeconds: b.WaitBetweenBatchesSeconds,
		ReviewAfterEachBatch:      b.ReviewAfterEachBatch,
		AutoContinue:              b.AutoContinueOnHealthyTelemetry,
		StabilizationSeconds:      b.StabilizationSeconds,
		MaxConcurrentOffline:      b.MaxConcurrentOffline,
		ControllerTimeoutSeconds:  b.ControllerTimeoutSeconds,
	}
	if b.Method != pb.RolloutMethod_ROLLOUT_METHOD_UNSPECIFIED && out.Method == "" {
		out.Method = b.Method.String()
	}
	if b.Order != pb.RolloutOrder_ROLLOUT_ORDER_UNSPECIFIED && out.Order == "" {
		out.Order = b.Order.String()
	}
	if t := b.Thresholds; t != nil {
		out.Thresholds = rollout.Thresholds{
			MaxHashrateDropPercent:       t.MaxHashrateDropPercent,
			MaxEfficiencyIncreasePercent: t.MaxEfficiencyIncreasePercent,
			MaxTempIncreaseC:             t.MaxTemperatureIncreaseCelsius,
			MaxNewErrors:                 t.MaxNewErrors,
			MinSampleCoveragePercent:     t.MinSampleCoveragePercent,
		}
	}
	return out
}

// overrideFromProto maps an optional behavior override; nil stays nil so the
// domain falls back to the channel's behavior.
func overrideFromProto(b *pb.RolloutBehavior) *rollout.Behavior {
	if b == nil {
		return nil
	}
	out := behaviorFromProto(b)
	return &out
}

func assignmentsFromProto(in []*pb.FirmwareAssignment) []rollout.Assignment {
	out := make([]rollout.Assignment, 0, len(in))
	for _, a := range in {
		out = append(out, rollout.Assignment{Manufacturer: a.Manufacturer, Model: a.Model, FirmwareFileID: a.FirmwareFileId})
	}
	return out
}

func behaviorToProto(b rollout.Behavior) *pb.RolloutBehavior {
	return &pb.RolloutBehavior{
		Method:                         methodToProto[b.Method],
		Order:                          orderToProto[b.Order],
		BatchSize:                      b.BatchSize,
		PilotSize:                      b.PilotSize,
		WaitBetweenBatchesSeconds:      b.WaitBetweenBatchesSeconds,
		ReviewAfterEachBatch:           b.ReviewAfterEachBatch,
		AutoContinueOnHealthyTelemetry: b.AutoContinue,
		StabilizationSeconds:           b.StabilizationSeconds,
		MaxConcurrentOffline:           b.MaxConcurrentOffline,
		ControllerTimeoutSeconds:       b.ControllerTimeoutSeconds,
		Thresholds: &pb.RolloutAutomationThresholds{
			MaxHashrateDropPercent:        b.Thresholds.MaxHashrateDropPercent,
			MaxEfficiencyIncreasePercent:  b.Thresholds.MaxEfficiencyIncreasePercent,
			MaxTemperatureIncreaseCelsius: b.Thresholds.MaxTempIncreaseC,
			MaxNewErrors:                  b.Thresholds.MaxNewErrors,
			MinSampleCoveragePercent:      b.Thresholds.MinSampleCoveragePercent,
		},
	}
}

func channelToProto(c *rollout.Channel) *pb.ReleaseChannel {
	return &pb.ReleaseChannel{
		Id:              c.ID,
		Name:            c.Name,
		Description:     c.Description,
		Scope:           scopeToProto(c.Scope),
		Behavior:        behaviorToProto(c.Behavior),
		ModelGroupCount: c.ModelGroupCount,
		MinerCount:      c.MinerCount,
		CreatedAt:       timestamppb.New(c.CreatedAt),
		UpdatedAt:       timestamppb.New(c.UpdatedAt),
	}
}

func channelSummaryToProto(c *rollout.Channel) *pb.ReleaseChannelSummary {
	return &pb.ReleaseChannelSummary{
		Id:              c.ID,
		Name:            c.Name,
		Description:     c.Description,
		Behavior:        behaviorToProto(c.Behavior),
		ModelGroupCount: c.ModelGroupCount,
		MinerCount:      c.MinerCount,
		CreatedAt:       timestamppb.New(c.CreatedAt),
		UpdatedAt:       timestamppb.New(c.UpdatedAt),
	}
}

func modelGroupToProto(g *rollout.ModelGroup) *pb.ReleaseChannelModelGroup {
	return &pb.ReleaseChannelModelGroup{
		Manufacturer:               g.Manufacturer,
		Model:                      g.Model,
		FirmwareFileId:             g.FirmwareFileID,
		FirmwareVersion:            g.FirmwareVersion,
		MinerCount:                 g.MinerCount,
		ActiveRolloutId:            g.ActiveRolloutID,
		OnTargetCount:              g.OnTargetCount,
		ReportedVersions:           g.ReportedVersions,
		ReportedVersionCount:       g.ReportedVersionCount,
		FirmwareTargetManufacturer: g.FirmwareTargetManufacturer,
		FirmwareTargetModel:        g.FirmwareTargetModel,
		AssignmentGeneration:       g.AssignmentGeneration,
		FirmwareChecksum:           g.FirmwareChecksum,
		FirmwareAvailable:          g.FirmwareAvailable,
	}
}

func minerToProto(m *rollout.ChannelMiner) *pb.ReleaseChannelMiner {
	return &pb.ReleaseChannelMiner{
		DeviceId:                     m.DeviceID,
		DeviceIdentifier:             m.DeviceIdentifier,
		Manufacturer:                 m.Manufacturer,
		Model:                        m.Model,
		FirmwareVersion:              m.FirmwareVersion,
		Conflicted:                   m.Conflicted,
		LastDeployedFirmwareChecksum: m.LastDeployedFirmwareChecksum,
	}
}

func conflictToProto(c *rollout.MembershipConflict) *pb.ReleaseChannelMembershipConflict {
	return &pb.ReleaseChannelMembershipConflict{
		DeviceId:            c.DeviceID,
		DeviceIdentifier:    c.DeviceIdentifier,
		Manufacturer:        c.Manufacturer,
		Model:               c.Model,
		ChannelId:           c.ChannelID,
		ChannelName:         c.ChannelName,
		SelectorSpecificity: pb.ReleaseChannelSelectorSpecificity(c.Specificity),
		Resolution:          resolutionToProto[c.Resolution],
	}
}

func planToProto(p *rollout.FirmwarePlan) *pb.ReleaseChannelFirmwarePlan {
	return &pb.ReleaseChannelFirmwarePlan{
		Manufacturer:     p.Pair.Manufacturer,
		Model:            p.Pair.Model,
		FirmwareFileId:   p.FirmwareFileID,
		FirmwareVersion:  p.FirmwareVersion,
		FirmwareChecksum: p.FirmwareChecksum,
		TargetCount:      p.TargetCount,
		OnTargetCount:    p.OnTargetCount,
		BatchCount:       p.BatchCount,
		Behavior:         behaviorToProto(p.Behavior),
		Unchanged:        p.Unchanged,
	}
}

func actorToProto(a rollout.Actor) *pb.RolloutActor {
	return &pb.RolloutActor{Type: actorTypeToProto[a.Type], Id: a.ID, Name: a.Name}
}

func errorInfoToProto(info *rollout.ErrorInfo) *pb.RolloutErrorInfo {
	return &pb.RolloutErrorInfo{
		Reason:            reasonToProto[info.Reason],
		CurrentRevision:   info.CurrentRevision,
		DeviceIdentifiers: info.DeviceIdentifiers,
	}
}

func deviceCountsToProto(c rollout.DeviceCounts) *pb.RolloutDeviceCounts {
	return &pb.RolloutDeviceCounts{
		Queued:     c.Queued,
		InProgress: c.InProgress,
		Retrying:   c.Retrying,
		Done:       c.Done,
		Failed:     c.Failed,
		Excluded:   c.Excluded,
		Skipped:    c.Skipped,
	}
}

func deviceToProto(d *rollout.RolloutDevice) *pb.RolloutDevice {
	return &pb.RolloutDevice{
		DeviceId:           d.DeviceID,
		DeviceIdentifier:   d.DeviceIdentifier,
		IpAddress:          d.IPAddress,
		FirmwareVersion:    d.FirmwareVersion,
		Phase:              phaseToProto[d.Phase],
		Batch:              d.Batch,
		Status:             d.Status,
		Online:             d.Online,
		Hashing:            d.Hashing,
		HasBaseline:        d.HasBaseline,
		BaselineHashing:    d.BaselineHashing,
		OpenErrors:         d.OpenErrors,
		BaselineOpenErrors: d.BaselineOpenErrors,
		HashRateHs:         metricToProto(d.HashRateHs),
		PowerW:             metricToProto(d.PowerW),
		EfficiencyJh:       metricToProto(d.EfficiencyJh),
		TempC:              metricToProto(d.TempC),
		Attempts:           d.Attempts,
		LastSentAt:         optionalTimestamp(d.LastSentAt),
		LastError:          d.LastError,
		SkipNote:           d.SkipNote,

		LastDeployedFirmwareChecksum: d.LastDeployedFirmwareChecksum,
	}
}

func metricToProto(m rollout.Metric) *pb.MetricComparison {
	return &pb.MetricComparison{Baseline: m.Baseline, Current: m.Current}
}

func aggregateToProto(a rollout.Aggregate) *pb.AggregateMetricComparison {
	return &pb.AggregateMetricComparison{Baseline: a.Baseline, Current: a.Current, SampledDevices: a.SampledDevices}
}

func optionalTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func rolloutToProto(r *rollout.Rollout) *pb.Rollout {
	out := &pb.Rollout{
		Id:                       r.ID,
		ChannelId:                r.ChannelID,
		ChannelName:              r.ChannelName,
		Manufacturer:             r.Manufacturer,
		Model:                    r.Model,
		FirmwareFileId:           r.FirmwareFileID,
		FirmwareChecksum:         r.FirmwareChecksum,
		FirmwareVersion:          r.FirmwareVersion,
		Status:                   statusToProto[r.Status],
		State:                    stateToProto[r.State],
		Stage:                    stageToProto[r.Stage],
		CancelReason:             cancelReasonToProto[r.CancelReason],
		Behavior:                 behaviorToProto(r.Behavior),
		BatchCount:               r.BatchCount,
		CurrentBatch:             r.CurrentBatch,
		StageChangedAt:           timestamppb.New(r.StageChangedAt),
		PausedAt:                 optionalTimestamp(r.PausedAt),
		CreatedAt:                timestamppb.New(r.CreatedAt),
		FinishedAt:               optionalTimestamp(r.FinishedAt),
		PreviousFirmwareChecksum: r.PreviousFirmwareChecksum,
		PreviousFirmwareVersion:  r.PreviousFirmwareVersion,
		AssignmentGeneration:     r.AssignmentGeneration,
		Revision:                 r.Revision,
		UpdatedAt:                timestamppb.New(r.UpdatedAt),
		StartedBy:                actorToProto(r.StartedBy),
		LastActionBy:             actorToProto(r.LastActionBy),
		// Devices are paged through ListRolloutDevices; the summary carries
		// the counts.
		DeviceCount:        int32(len(r.Devices)), //nolint:gosec // bounded by the snapshot size
		DeviceCounts:       deviceCountsToProto(r.DeviceCounts),
		CurrentBatchCounts: deviceCountsToProto(r.CurrentBatchCounts),
	}
	if ev := r.Evidence; ev != nil {
		out.Evidence = &pb.RolloutEvidence{
			DevicesTotal:                  ev.DevicesTotal,
			Verified:                      ev.Verified,
			Online:                        ev.Online,
			Hashing:                       ev.Hashing,
			BaselineHashing:               ev.BaselineHashing,
			Failed:                        ev.Failed,
			Excluded:                      ev.Excluded,
			Skipped:                       ev.Skipped,
			HashrateChangePercent:         ev.HashrateChangePercent,
			EfficiencyChangePercent:       ev.EfficiencyChangePercent,
			TemperatureChangeCelsius:      ev.TemperatureChangeC,
			NewErrors:                     ev.NewErrors,
			ReadyToAdvance:                ev.ReadyToAdvance,
			HoldReason:                    ev.HoldReason,
			StabilizationRemainingSeconds: ev.StabilizationRemainingSeconds,
			HashRateHs:                    aggregateToProto(ev.HashRateHs),
			PowerW:                        aggregateToProto(ev.PowerW),
			EfficiencyJh:                  aggregateToProto(ev.EfficiencyJh),
			TempC:                         aggregateToProto(ev.TempC),
		}
	}
	return out
}
