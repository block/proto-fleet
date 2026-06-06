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

// curtailmentService is the subset of curtailment.Service the driver needs.
type curtailmentService interface {
	Start(ctx context.Context, req curtailment.StartRequest) (*curtailment.Plan, error)
	Stop(ctx context.Context, req curtailment.StopRequest) (*models.Event, error)
	// ListActive returns all non-terminal events; source_actor_id identifies
	// this MQTT source among concurrent per-scope events.
	ListActive(ctx context.Context, orgID int64) ([]*models.Event, error)
	// Recurtail routes a restoring event back through curtail dispatch.
	Recurtail(ctx context.Context, req curtailment.RecurtailRequest) (*models.Event, error)
}

// EdgeOutcome reports the result of dispatching one edge. The subscriber uses
// EventUUID for last_edge_event_uuid and derives last_edge_at from pending
// edge receive time.
type EdgeOutcome struct {
	// EventUUID is the curtailment event the edge created (ON→OFF and
	// WATCHDOG_OFF) or stopped (OFF→ON). Zero for EdgeNone.
	EventUUID uuid.UUID
	// EmptyFullFleetNoop means Start completed immediately with no targets.
	EmptyFullFleetNoop bool
}

// Driver translates MQTT edges into curtailment service calls.
type Driver struct {
	svc curtailmentService
}

// NewDriver returns a driver wired to the given service.
func NewDriver(svc curtailmentService) *Driver {
	return &Driver{svc: svc}
}

// Dispatch routes an edge and returns the event it created, resumed, or stopped.
// priorEdgeAt salts message-driven OFF external references.
func (d *Driver) Dispatch(ctx context.Context, src SourceConfig, direction EdgeDirection, edgeAt time.Time, priorEdgeAt ...time.Time) (EdgeOutcome, error) {
	var prior time.Time
	if len(priorEdgeAt) > 0 {
		prior = priorEdgeAt[0]
	}
	switch direction {
	case EdgeNone:
		return EdgeOutcome{}, nil

	case EdgeOnToOff, EdgeWatchdogOff:
		eventUUID, emptyFullFleetNoop, err := d.dispatchCurtail(ctx, src, direction, edgeAt, prior)
		if err != nil {
			return EdgeOutcome{}, err
		}
		return EdgeOutcome{
			EventUUID:          eventUUID,
			EmptyFullFleetNoop: emptyFullFleetNoop,
		}, nil

	case EdgeOffToOn:
		event, err := d.dispatchStop(ctx, src)
		if err != nil {
			return EdgeOutcome{}, err
		}
		return EdgeOutcome{
			EventUUID: event.EventUUID,
		}, nil

	default:
		return EdgeOutcome{}, fmt.Errorf("mqttingest: unknown edge direction %d", direction)
	}
}

func (d *Driver) dispatchCurtail(ctx context.Context, src SourceConfig, direction EdgeDirection, edgeAt, priorEdgeAt time.Time) (uuid.UUID, bool, error) {
	active, err := d.ActiveSourceEvent(ctx, src)
	if err != nil {
		return uuid.Nil, false, err
	}
	switch {
	case eventIsRestoring(active):
		if err := d.ResumeSourceEvent(ctx, active); err != nil {
			return uuid.Nil, false, err
		}
		return active.EventUUID, false, nil
	case eventHoldsCurtailment(active):
		return active.EventUUID, false, nil
	}
	return d.dispatchStart(ctx, src, direction, edgeAt, priorEdgeAt)
}

func (d *Driver) dispatchStart(ctx context.Context, src SourceConfig, direction EdgeDirection, edgeAt, priorEdgeAt time.Time) (uuid.UUID, bool, error) {
	scope, err := scopeForSource(src)
	if err != nil {
		return uuid.Nil, false, err
	}

	externalRef := startExternalReference(src.SourceName, direction, edgeAt, priorEdgeAt, src.StalenessThreshold)
	reason := startReason(src.SourceName, direction, edgeAt)

	externalSource := src.SourceName
	sourceActorID := sourceActorIDFor(src)

	mode, targetKW, toleranceKW := modeForSource(src)
	req := curtailment.StartRequest{
		PreviewRequest: curtailment.PreviewRequest{
			OrgID:       src.OrganizationID,
			Scope:       scope,
			Mode:        mode,
			Strategy:    models.StrategyLeastEfficientFirst,
			Level:       models.LevelFull,
			Priority:    models.PriorityEmergency,
			TargetKW:    targetKW,
			ToleranceKW: toleranceKW,
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
		// Retryable errors stay retryable; idempotent re-deliveries return
		// plan.ReplayEvent instead.
		return uuid.Nil, false, fmt.Errorf("mqttingest: dispatch Start: %w", err)
	}
	if plan == nil {
		return uuid.Nil, false, errors.New("mqttingest: curtailment service returned nil plan on Start")
	}
	if plan.ReplayEvent != nil {
		if eventIsRestoring(plan.ReplayEvent) {
			if err := d.ResumeSourceEvent(ctx, plan.ReplayEvent); err != nil {
				return uuid.Nil, false, err
			}
		}
		return plan.ReplayEvent.EventUUID, false, nil
	}
	if plan.InsufficientLoadDetail != nil {
		return uuid.Nil, false, fmt.Errorf("mqttingest: curtailment service rejected Start (insufficient load): %+v", plan.InsufficientLoadDetail)
	}
	if plan.EventUUID == nil {
		return uuid.Nil, false, errors.New("mqttingest: curtailment service returned plan with no event UUID")
	}
	return *plan.EventUUID, mode == models.ModeFullFleet && len(plan.Selected) == 0, nil
}

func (d *Driver) dispatchStop(ctx context.Context, src SourceConfig) (*models.Event, error) {
	active, err := d.ActiveSourceEvent(ctx, src)
	if err != nil {
		return nil, err
	}
	if active == nil {
		return nil, ErrNoActiveEvent
	}
	stopReq := curtailment.StopRequest{
		OrgID:     src.OrganizationID,
		EventUUID: active.EventUUID,
		// MQTT ON is authoritative; source min-hold must not block restore.
		Force: true,
	}
	event, err := d.svc.Stop(ctx, stopReq)
	if err != nil {
		// If the event went terminal between lookup and Stop, ON can settle.
		// A still non-terminal event means Stop genuinely failed and must retry.
		if active2, rerr := d.ActiveSourceEvent(ctx, src); rerr == nil && active2 == nil {
			return nil, ErrNoActiveEvent
		}
		return nil, fmt.Errorf("mqttingest: dispatch Stop: %w", err)
	}
	if event == nil {
		return nil, errors.New("mqttingest: curtailment service returned nil event on Stop")
	}
	return event, nil
}

// ActiveSourceEvent returns this source's non-terminal event, if any.
func (d *Driver) ActiveSourceEvent(ctx context.Context, src SourceConfig) (*models.Event, error) {
	events, err := d.svc.ListActive(ctx, src.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("mqttingest: ListActive: %w", err)
	}
	want := sourceActorIDFor(src)
	for _, ev := range events {
		if ev != nil && ev.SourceActorID != nil && *ev.SourceActorID == want {
			return ev, nil
		}
	}
	return nil, nil
}

// ResumeSourceEvent re-curtails a restoring source event in place.
func (d *Driver) ResumeSourceEvent(ctx context.Context, event *models.Event) error {
	if _, err := d.svc.Recurtail(ctx, curtailment.RecurtailRequest{
		OrgID:     event.OrgID,
		EventUUID: event.EventUUID,
	}); err != nil {
		return fmt.Errorf("mqttingest: recurtail: %w", err)
	}
	return nil
}

func eventHoldsCurtailment(event *models.Event) bool {
	if event == nil {
		return false
	}
	return event.State == models.EventStatePending || event.State == models.EventStateActive
}

func eventIsRestoring(event *models.Event) bool {
	return event != nil && event.State == models.EventStateRestoring
}

// ErrNoActiveEvent is returned by Dispatch on OFF→ON when no
// non-terminal event exists. Caller treats this as a benign no-op
// (the subscriber's edge bookkeeping still moves to ON).
var ErrNoActiveEvent = errors.New("mqttingest: no active event to stop")

// clampToInt32Seconds converts a duration to int32 seconds, saturating
// rather than wrapping on an outsized (operator-typo) value.
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

// startExternalReference builds the idempotency key for OFF dispatches.
func startExternalReference(source string, direction EdgeDirection, edgeAt, priorEdgeAt time.Time, stalenessThreshold time.Duration) string {
	if direction == EdgeWatchdogOff {
		thresholdSec := int64(stalenessThreshold / time.Second)
		if thresholdSec <= 0 {
			thresholdSec = 1
		}
		windowStart := (edgeAt.Unix() / thresholdSec) * thresholdSec
		return fmt.Sprintf("%s:watchdog:%d", source, windowStart)
	}
	// Salt with the prior edge so same-second OFF bursts do not replay each other.
	if priorEdgeAt.IsZero() {
		return fmt.Sprintf("%s:%d", source, edgeAt.Unix())
	}
	return fmt.Sprintf("%s:%d:%d", source, edgeAt.Unix(), priorEdgeAt.Unix())
}

// startReason builds the operator-facing event reason.
func startReason(source string, direction EdgeDirection, edgeAt time.Time) string {
	if direction == EdgeWatchdogOff {
		return fmt.Sprintf("MQTT watchdog — source %s, last message before %s", source, edgeAt.Format(time.RFC3339))
	}
	return fmt.Sprintf("MQTT OFF target — source %s", source)
}

// sourceActorIDFor is stamped on events this MQTT source owns.
func sourceActorIDFor(src SourceConfig) string {
	return fmt.Sprintf("mqtt:%s", src.SourceName)
}

// modeForSource maps source config to curtailment mode parameters.
func modeForSource(src SourceConfig) (mode models.Mode, targetKW, toleranceKW float64) {
	if src.CurtailMode == "" || models.Mode(src.CurtailMode) == models.ModeFullFleet {
		return models.ModeFullFleet, 0, 0
	}
	kw := float64(src.ContractedCurtailmentKw)
	return models.ModeFixedKw, kw, kw * 0.05
}

// scopeForSource maps source config to a curtailment scope.
func scopeForSource(src SourceConfig) (curtailment.Scope, error) {
	switch src.ScopeType {
	case string(models.ScopeTypeWholeOrg), "":
		return curtailment.Scope{Type: models.ScopeTypeWholeOrg}, nil
	case string(models.ScopeTypeDeviceList):
		if len(src.ScopeDeviceIdentifiers) == 0 {
			return curtailment.Scope{}, fmt.Errorf("mqttingest: device_list scope for source %q has no device identifiers", src.SourceName)
		}
		return curtailment.Scope{
			Type:              models.ScopeTypeDeviceList,
			DeviceIdentifiers: src.ScopeDeviceIdentifiers,
		}, nil
	default:
		return curtailment.Scope{}, fmt.Errorf("mqttingest: unsupported scope type %q for source %q", src.ScopeType, src.SourceName)
	}
}
