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
	}
	methodFromProto = map[pb.RolloutMethod]string{
		pb.RolloutMethod_ROLLOUT_METHOD_ALL_AT_ONCE:         rollout.MethodAllAtOnce,
		pb.RolloutMethod_ROLLOUT_METHOD_BATCHED:             rollout.MethodBatched,
		pb.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE: rollout.MethodPilotThenContinue,
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
	stateToProto = map[string]pb.RolloutState{
		rollout.StateInProgress:            pb.RolloutState_ROLLOUT_STATE_IN_PROGRESS,
		rollout.StateStabilizingTelemetry:  pb.RolloutState_ROLLOUT_STATE_STABILIZING_TELEMETRY,
		rollout.StatePausedAtPilotGate:     pb.RolloutState_ROLLOUT_STATE_PAUSED_AT_PILOT_GATE,
		rollout.StatePausedAtBatchReview:   pb.RolloutState_ROLLOUT_STATE_PAUSED_AT_BATCH_REVIEW,
		rollout.StatePaused:                pb.RolloutState_ROLLOUT_STATE_PAUSED,
		rollout.StateCompleted:             pb.RolloutState_ROLLOUT_STATE_COMPLETED,
		rollout.StateCompletedWithFailures: pb.RolloutState_ROLLOUT_STATE_COMPLETED_WITH_FAILURES,
		rollout.StateCanceled:              pb.RolloutState_ROLLOUT_STATE_CANCELED,
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
		}
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
		Thresholds: &pb.RolloutAutomationThresholds{
			MaxHashrateDropPercent:        b.Thresholds.MaxHashrateDropPercent,
			MaxEfficiencyIncreasePercent:  b.Thresholds.MaxEfficiencyIncreasePercent,
			MaxTemperatureIncreaseCelsius: b.Thresholds.MaxTempIncreaseC,
			MaxNewErrors:                  b.Thresholds.MaxNewErrors,
		},
	}
}

func channelToProto(c *rollout.Channel) *pb.ReleaseChannel {
	out := &pb.ReleaseChannel{
		Id:          c.ID,
		Name:        c.Name,
		Description: c.Description,
		Scope:       scopeToProto(c.Scope),
		Behavior:    behaviorToProto(c.Behavior),
		MinerCount:  c.MinerCount,
		CreatedAt:   timestamppb.New(c.CreatedAt),
		UpdatedAt:   timestamppb.New(c.UpdatedAt),
	}
	for _, g := range c.ModelGroups {
		group := &pb.ReleaseChannelModelGroup{
			Model:           g.Model,
			FirmwareFileId:  g.FirmwareFileID,
			FirmwareVersion: g.FirmwareVersion,
			ActiveRolloutId: g.ActiveRolloutID,
		}
		for _, m := range g.Miners {
			group.Miners = append(group.Miners, &pb.ReleaseChannelMiner{
				DeviceId:         m.DeviceID,
				DeviceIdentifier: m.DeviceIdentifier,
				Model:            m.Model,
				FirmwareVersion:  m.FirmwareVersion,
				Conflicted:       m.Conflicted,
			})
		}
		out.ModelGroups = append(out.ModelGroups, group)
	}
	return out
}

func metricToProto(m rollout.Metric) *pb.MetricComparison {
	return &pb.MetricComparison{Baseline: m.Baseline, Current: m.Current}
}

func optionalTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func rolloutToProto(r *rollout.Rollout) *pb.Rollout {
	out := &pb.Rollout{
		Id:                      r.ID,
		ChannelId:               r.ChannelID,
		ChannelName:             r.ChannelName,
		Model:                   r.Model,
		FirmwareFileId:          r.FirmwareFileID,
		FirmwareVersion:         r.FirmwareVersion,
		Status:                  statusToProto[r.Status],
		State:                   stateToProto[r.State],
		Stage:                   stageToProto[r.Stage],
		CancelReason:            cancelReasonToProto[r.CancelReason],
		Behavior:                behaviorToProto(r.Behavior),
		BatchCount:              r.BatchCount,
		CurrentBatch:            r.CurrentBatch,
		StageChangedAt:          timestamppb.New(r.StageChangedAt),
		PausedAt:                optionalTimestamp(r.PausedAt),
		CreatedAt:               timestamppb.New(r.CreatedAt),
		FinishedAt:              optionalTimestamp(r.FinishedAt),
		PreviousFirmwareFileId:  r.PreviousFirmwareFileID,
		PreviousFirmwareVersion: r.PreviousFirmwareVersion,
	}
	for _, d := range r.Devices {
		out.Devices = append(out.Devices, &pb.RolloutDevice{
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
		})
	}
	if ev := r.Evidence; ev != nil {
		out.Evidence = &pb.RolloutEvidence{
			DevicesTotal:                  ev.DevicesTotal,
			Verified:                      ev.Verified,
			Online:                        ev.Online,
			Hashing:                       ev.Hashing,
			BaselineHashing:               ev.BaselineHashing,
			Failed:                        ev.Failed,
			HashrateChangePercent:         ev.HashrateChangePercent,
			EfficiencyChangePercent:       ev.EfficiencyChangePercent,
			TemperatureChangeCelsius:      ev.TemperatureChangeC,
			NewErrors:                     ev.NewErrors,
			ReadyToAdvance:                ev.ReadyToAdvance,
			HoldReason:                    ev.HoldReason,
			StabilizationRemainingSeconds: ev.StabilizationRemainingSeconds,
			HashRateHs:                    metricToProto(ev.HashRateHs),
			PowerW:                        metricToProto(ev.PowerW),
			EfficiencyJh:                  metricToProto(ev.EfficiencyJh),
			TempC:                         metricToProto(ev.TempC),
		}
	}
	return out
}
