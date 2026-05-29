package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// curtailmentService is the subset of curtailment.Service the driver
// needs. Narrow interface keeps the driver testable with a fake.
type curtailmentService interface {
	Start(ctx context.Context, req curtailment.StartRequest) (*curtailment.Plan, error)
	Stop(ctx context.Context, req curtailment.StopRequest) (*models.Event, error)
	GetActive(ctx context.Context, orgID int64) (*models.Event, error)
}

// EdgeOutcome reports the result of dispatching one edge. The
// subscriber consumes this to update persisted state — specifically
// last_edge_at and last_edge_event_uuid.
type EdgeOutcome struct {
	Direction EdgeDirection
	// EventUUID is the curtailment event the edge created (ON→OFF and
	// WATCHDOG_OFF) or stopped (OFF→ON). Zero for EdgeNone.
	EventUUID uuid.UUID
	// DispatchedAt is the wall-clock time the edge was dispatched.
	DispatchedAt time.Time
}

// Driver translates edge decisions into Service.Start / Service.Stop
// invocations against the curtailment service.
type Driver struct {
	svc curtailmentService
	now func() time.Time
}

// NewDriver returns a driver wired to the given curtailment service.
// `now` is the clock the driver stamps onto outgoing requests; pass
// time.Now in production, an injected clock in tests.
func NewDriver(svc curtailmentService, now func() time.Time) *Driver {
	if now == nil {
		now = time.Now
	}
	return &Driver{svc: svc, now: now}
}

// Dispatch routes an edge to the appropriate curtailment-service call
// and returns the resulting EventUUID. EdgeNone is a no-op that
// returns a zero outcome.
func (d *Driver) Dispatch(ctx context.Context, src SourceConfig, direction EdgeDirection, edgeAt time.Time) (EdgeOutcome, error) {
	switch direction {
	case EdgeNone:
		return EdgeOutcome{Direction: EdgeNone}, nil

	case EdgeOnToOff, EdgeWatchdogOff:
		eventUUID, err := d.dispatchStart(ctx, src, direction, edgeAt)
		if err != nil {
			return EdgeOutcome{}, err
		}
		return EdgeOutcome{
			Direction:    direction,
			EventUUID:    eventUUID,
			DispatchedAt: d.now().UTC(),
		}, nil

	case EdgeOffToOn:
		event, err := d.dispatchStop(ctx, src)
		if err != nil {
			return EdgeOutcome{}, err
		}
		return EdgeOutcome{
			Direction:    EdgeOffToOn,
			EventUUID:    event.EventUUID,
			DispatchedAt: d.now().UTC(),
		}, nil

	default:
		return EdgeOutcome{}, fmt.Errorf("mqttingest: unknown edge direction %d", direction)
	}
}

func (d *Driver) dispatchStart(ctx context.Context, src SourceConfig, direction EdgeDirection, edgeAt time.Time) (uuid.UUID, error) {
	externalRef := startExternalReference(src.SourceName, direction, edgeAt, src.StalenessThreshold)
	reason := startReason(src.SourceName, direction, edgeAt)

	externalSource := src.SourceName
	sourceActorID := fmt.Sprintf("mqtt:%s", src.SourceName)

	req := curtailment.StartRequest{
		PreviewRequest: curtailment.PreviewRequest{
			OrgID: src.OrganizationID,
			Scope: curtailment.Scope{
				Type: models.ScopeTypeWholeOrg,
			},
			Mode:        models.ModeFixedKw,
			Strategy:    models.StrategyLeastEfficientFirst,
			Level:       models.LevelFull,
			Priority:    models.PriorityEmergency,
			TargetKW:    float64(src.ContractedCurtailmentKw),
			ToleranceKW: float64(src.ContractedCurtailmentKw) * 0.05,
		},
		Reason:                  reason,
		MinCurtailedDurationSec: clampToInt32Seconds(src.MinCurtailedDuration),
		AllowUnbounded:          true,
		CanUseAdminControls:     true,
		ExternalSource:          &externalSource,
		ExternalReference:       &externalRef,
		SourceActorType:         models.SourceActorWebhook,
		SourceActorID:           &sourceActorID,
		CreatedByUserID:         src.ServiceUserID,
	}

	plan, err := d.svc.Start(ctx, req)
	if err != nil {
		return uuid.Nil, fmt.Errorf("mqttingest: dispatch Start: %w", err)
	}
	if plan == nil {
		return uuid.Nil, errors.New("mqttingest: curtailment service returned nil plan on Start")
	}
	// Replay path: the partial unique index hit; the persisted event is
	// returned verbatim.
	if plan.ReplayEvent != nil {
		return plan.ReplayEvent.EventUUID, nil
	}
	// Insufficient-load outcome: the selector decided there is no
	// dispatchable load. Surface as an error so the subscriber doesn't
	// commit an edge that didn't actually curtail anything.
	if plan.InsufficientLoadDetail != nil {
		return uuid.Nil, fmt.Errorf("mqttingest: curtailment service rejected Start (insufficient load): %+v", plan.InsufficientLoadDetail)
	}
	if plan.EventUUID == nil {
		return uuid.Nil, errors.New("mqttingest: curtailment service returned plan with no event UUID")
	}
	return *plan.EventUUID, nil
}

func (d *Driver) dispatchStop(ctx context.Context, src SourceConfig) (*models.Event, error) {
	active, err := d.svc.GetActive(ctx, src.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("mqttingest: GetActive on OFF→ON: %w", err)
	}
	if active == nil {
		// Restorer-side state already final; treat as no-op success.
		return nil, ErrNoActiveEvent
	}
	stopReq := curtailment.StopRequest{
		OrgID:     src.OrganizationID,
		EventUUID: active.EventUUID,
	}
	event, err := d.svc.Stop(ctx, stopReq)
	if err != nil {
		return nil, fmt.Errorf("mqttingest: dispatch Stop: %w", err)
	}
	return event, nil
}

// ErrNoActiveEvent is returned by Dispatch on OFF→ON when no
// non-terminal event exists. Caller treats this as a benign no-op
// (the subscriber's edge bookkeeping still moves to ON).
var ErrNoActiveEvent = errors.New("mqttingest: no active event to stop")

// clampToInt32Seconds converts a duration to an int32 seconds value
// with explicit upper-bound clamping. The curtailment service treats
// MinCurtailedDurationSec as int32; an outsized source-config value
// (operator typo) saturates rather than wrapping.
func clampToInt32Seconds(d time.Duration) int32 {
	const maxInt32 = int64(1<<31 - 1)
	secs := int64(d / time.Second)
	if secs < 0 {
		return 0
	}
	if secs > maxInt32 {
		return int32(maxInt32)
	}
	return int32(secs)
}

// startExternalReference synthesizes the per-edge external_reference
// the curtailment service uses for idempotency. Format keeps the v1
// partial-unique index dedupe working across broker-pair race and
// fleetd restart-near-edge. Only called for ON->OFF and WATCHDOG_OFF.
//
// Watchdog references are quantized to the source's staleness threshold
// so back-to-back ticks within the same stale episode produce the same
// reference. Without this, a 1 s watchdog tick generates a fresh
// external_reference every second; the partial-unique index would not
// see them as replays and the curtailment service would run a full
// selector pass each tick.
func startExternalReference(source string, direction EdgeDirection, edgeAt time.Time, stalenessThreshold time.Duration) string {
	if direction == EdgeWatchdogOff {
		thresholdSec := int64(stalenessThreshold / time.Second)
		if thresholdSec <= 0 {
			thresholdSec = 1
		}
		windowStart := (edgeAt.Unix() / thresholdSec) * thresholdSec
		return fmt.Sprintf("%s:watchdog:%d", source, windowStart)
	}
	return fmt.Sprintf("%s:%d", source, edgeAt.Unix())
}

// startReason builds the operator-facing reason text recorded on the
// curtailment event. The two trigger modes (publisher OFF vs.
// staleness watchdog) get distinct phrasing so audit-log readers can
// distinguish at a glance. Only called for ON->OFF and WATCHDOG_OFF.
func startReason(source string, direction EdgeDirection, edgeAt time.Time) string {
	if direction == EdgeWatchdogOff {
		return fmt.Sprintf("MQTT watchdog — source %s, last message before %s", source, edgeAt.Format(time.RFC3339))
	}
	return fmt.Sprintf("MQTT OFF target — source %s", source)
}
