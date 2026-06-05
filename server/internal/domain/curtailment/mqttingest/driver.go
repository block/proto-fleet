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
	// ListActive returns all non-terminal events for the org. The driver
	// matches source_actor_id to find this source's event among concurrent
	// per-scope events (GetActive returns only the most-recent, which can be
	// another source's).
	ListActive(ctx context.Context, orgID int64) ([]*models.Event, error)
	// Recurtail flips a restoring event back to active and re-curtails its
	// in-flight targets in place; the watchdog uses it to re-assert OFF without
	// starting a fresh event (which would replay the restoring one).
	Recurtail(ctx context.Context, req curtailment.RecurtailRequest) (*models.Event, error)
}

// EdgeOutcome reports the result of dispatching one edge; the subscriber
// uses it to update last_edge_at and last_edge_event_uuid.
type EdgeOutcome struct {
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

// NewDriver returns a driver wired to the given service. `now` is the
// clock for stamping outcomes (time.Now in prod, injected in tests).
func NewDriver(svc curtailmentService, now func() time.Time) *Driver {
	if now == nil {
		now = time.Now
	}
	return &Driver{svc: svc, now: now}
}

// Dispatch routes an edge to the appropriate curtailment-service call
// and returns the resulting EventUUID. EdgeNone is a no-op that
// returns a zero outcome. The optional priorEdgeAt is the previous edge's
// anchor; it salts the message-driven external_reference so two OFF edges
// sharing a publisher second don't collide (see startExternalReference).
// Callers that don't need it (tests) may omit it.
func (d *Driver) Dispatch(ctx context.Context, src SourceConfig, direction EdgeDirection, edgeAt time.Time, priorEdgeAt ...time.Time) (EdgeOutcome, error) {
	var prior time.Time
	if len(priorEdgeAt) > 0 {
		prior = priorEdgeAt[0]
	}
	switch direction {
	case EdgeNone:
		return EdgeOutcome{}, nil

	case EdgeOnToOff, EdgeWatchdogOff:
		eventUUID, err := d.dispatchCurtail(ctx, src, direction, edgeAt, prior)
		if err != nil {
			return EdgeOutcome{}, err
		}
		return EdgeOutcome{
			EventUUID:    eventUUID,
			DispatchedAt: d.now().UTC(),
		}, nil

	case EdgeOffToOn:
		event, err := d.dispatchStop(ctx, src)
		if err != nil {
			return EdgeOutcome{}, err
		}
		return EdgeOutcome{
			EventUUID:    event.EventUUID,
			DispatchedAt: d.now().UTC(),
		}, nil

	default:
		return EdgeOutcome{}, fmt.Errorf("mqttingest: unknown edge direction %d", direction)
	}
}

func (d *Driver) dispatchCurtail(ctx context.Context, src SourceConfig, direction EdgeDirection, edgeAt, priorEdgeAt time.Time) (uuid.UUID, error) {
	// OFF means "curtail now" even if a previous ON has already started
	// restoring this source's event. Reuse the source event when it exists so
	// we do not fight the in-flight restore with a fresh Start.
	active, err := d.ActiveSourceEvent(ctx, src)
	if err != nil {
		return uuid.Nil, err
	}
	switch {
	case eventIsRestoring(active):
		if err := d.ResumeSourceEvent(ctx, active); err != nil {
			return uuid.Nil, err
		}
		return active.EventUUID, nil
	case eventHoldsCurtailment(active):
		return active.EventUUID, nil
	}
	return d.dispatchStart(ctx, src, direction, edgeAt, priorEdgeAt)
}

func (d *Driver) dispatchStart(ctx context.Context, src SourceConfig, direction EdgeDirection, edgeAt, priorEdgeAt time.Time) (uuid.UUID, error) {
	scope, err := scopeForSource(src)
	if err != nil {
		return uuid.Nil, err
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
		// Errors (including a device-overlap AlreadyExists race) propagate so
		// the worker logs and retries on the next message or watchdog tick.
		// Idempotent re-deliveries are not errors — they return plan.ReplayEvent.
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
	// Insufficient load: surface as an error so the subscriber doesn't
	// commit an edge that curtailed nothing.
	if plan.InsufficientLoadDetail != nil {
		return uuid.Nil, fmt.Errorf("mqttingest: curtailment service rejected Start (insufficient load): %+v", plan.InsufficientLoadDetail)
	}
	if plan.EventUUID == nil {
		return uuid.Nil, errors.New("mqttingest: curtailment service returned plan with no event UUID")
	}
	return *plan.EventUUID, nil
}

func (d *Driver) dispatchStop(ctx context.Context, src SourceConfig) (*models.Event, error) {
	// Stop only the event this source created; a nil or foreign active event
	// is not this OFF→ON's to stop, so treat it as a benign no-op (the source
	// still advances to ON).
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
	}
	event, err := d.svc.Stop(ctx, stopReq)
	if err != nil {
		// The event can go terminal between ActiveSourceEvent listing it and Stop
		// running (admin terminate, or its own restore completing); Stop rejects a
		// terminal event. If this source no longer has a non-terminal event there
		// is nothing left to stop, so treat it like ErrNoActiveEvent — the OFF→ON
		// still advances and the watchdog won't re-curtail a restored source. A
		// still-active event is a real failure (e.g. the min-curtailed-duration
		// gate, which also reports FailedPrecondition) — propagate it so the
		// worker retries rather than wrongly settling ON.
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

// ActiveSourceEvent returns the non-terminal curtailment event this source
// created, or nil when none exists or the active event belongs to another
// actor (a manual or cross-source curtailment).
func (d *Driver) ActiveSourceEvent(ctx context.Context, src SourceConfig) (*models.Event, error) {
	events, err := d.svc.ListActive(ctx, src.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("mqttingest: ListActive: %w", err)
	}
	// Multiple events can be active per org (one per disjoint scope); match
	// source_actor_id so this source's event is found even when it isn't the
	// most-recent.
	want := sourceActorIDFor(src)
	for _, ev := range events {
		if ev != nil && ev.SourceActorID != nil && *ev.SourceActorID == want {
			return ev, nil
		}
	}
	return nil, nil
}

// ResumeSourceEvent re-asserts curtailment on this source's restoring event
// (an out-of-band Stop began a restore while the publisher still signals OFF),
// flipping it back to active in place. Preferred over a fresh WATCHDOG_OFF
// Start, which would replay the restoring event instead of re-curtailing.
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

// startExternalReference synthesizes the per-edge external_reference used
// for the curtailment service's idempotency (partial-unique index).
// Watchdog references are quantized to the staleness threshold so
// back-to-back 1 s ticks in one stale episode share a reference and
// replay, instead of triggering a fresh selector pass each tick. Only
// called for ON->OFF and WATCHDOG_OFF.
func startExternalReference(source string, direction EdgeDirection, edgeAt, priorEdgeAt time.Time, stalenessThreshold time.Duration) string {
	if direction == EdgeWatchdogOff {
		thresholdSec := int64(stalenessThreshold / time.Second)
		if thresholdSec <= 0 {
			thresholdSec = 1
		}
		windowStart := (edgeAt.Unix() / thresholdSec) * thresholdSec
		return fmt.Sprintf("%s:watchdog:%d", source, windowStart)
	}
	// Salt the message-driven reference with the prior edge's second. Wire
	// stamps are seconds-precision, so an OFF→ON→OFF burst stamped in one second
	// but received outside the debounce window would otherwise give both OFFs
	// the same source:<second> reference; Stop leaves the first event
	// `restoring` (still covered by the unique index), so the second OFF would
	// be treated as a replay and dropped. The prior anchor is persisted state
	// (LastEdgeAt) and stays fixed across a redelivery of the same edge, so the
	// reference is still stable. Cold start (no prior edge) keeps the bare form.
	if priorEdgeAt.IsZero() {
		return fmt.Sprintf("%s:%d", source, edgeAt.Unix())
	}
	return fmt.Sprintf("%s:%d:%d", source, edgeAt.Unix(), priorEdgeAt.Unix())
}

// startReason builds the operator-facing reason recorded on the event,
// with distinct phrasing for publisher-OFF vs. watchdog triggers. Only
// called for ON->OFF and WATCHDOG_OFF.
func startReason(source string, direction EdgeDirection, edgeAt time.Time) string {
	if direction == EdgeWatchdogOff {
		return fmt.Sprintf("MQTT watchdog — source %s, last message before %s", source, edgeAt.Format(time.RFC3339))
	}
	return fmt.Sprintf("MQTT OFF target — source %s", source)
}

// sourceActorIDFor is the source_actor_id the driver stamps on every event it
// starts; the OFF→ON path uses it to confirm an active event belongs to this
// source before stopping it.
func sourceActorIDFor(src SourceConfig) string {
	return fmt.Sprintf("mqtt:%s", src.SourceName)
}

// modeForSource builds the curtailment mode and kW params from the source's
// curtail_mode. FULL_FLEET curtails every eligible device in scope with no kW
// target; FIXED_KW (the default) sheds the contracted target with a 5%
// undershoot tolerance. With a device_list scope, FULL_FLEET means "stop this
// whole site."
func modeForSource(src SourceConfig) (mode models.Mode, targetKW, toleranceKW float64) {
	if models.Mode(src.CurtailMode) == models.ModeFullFleet {
		return models.ModeFullFleet, 0, 0
	}
	kw := float64(src.ContractedCurtailmentKw)
	return models.ModeFixedKw, kw, kw * 0.05
}

// scopeForSource builds the curtailment Scope from the source config. Supports
// whole_org and device_list; device_sets is rejected (the curtailment core
// returns Unimplemented for it).
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
