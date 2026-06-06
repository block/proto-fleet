package mqttingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// fakeService captures driver service calls. Methods are mutexed for subscriber tests.
type fakeService struct {
	mu              sync.Mutex
	startCalls      []curtailment.StartRequest
	stopCalls       []curtailment.StopRequest
	listActiveCalls []int64

	recurtailCalls []curtailment.RecurtailRequest

	startResult      *curtailment.Plan
	startErr         error
	stopResult       *models.Event
	stopErr          error
	recurtailResult  *models.Event
	recurtailErr     error
	listActiveResult []*models.Event
	// listActiveResults, when set, returns a distinct result per ListActive call
	// (clamped to the last entry) so a test can model a TOCTOU race where the
	// event is listed first and gone on a re-check.
	listActiveResults [][]*models.Event
	listActiveErrs    []error
	listActiveErr     error
}

func (f *fakeService) Start(_ context.Context, req curtailment.StartRequest) (*curtailment.Plan, error) {
	f.mu.Lock()
	f.startCalls = append(f.startCalls, req)
	res, err := f.startResult, f.startErr
	f.mu.Unlock()
	return res, err
}

func (f *fakeService) Stop(_ context.Context, req curtailment.StopRequest) (*models.Event, error) {
	f.mu.Lock()
	f.stopCalls = append(f.stopCalls, req)
	res, err := f.stopResult, f.stopErr
	f.mu.Unlock()
	return res, err
}

func (f *fakeService) ListActive(_ context.Context, orgID int64) ([]*models.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx := len(f.listActiveCalls)
	f.listActiveCalls = append(f.listActiveCalls, orgID)
	if f.listActiveErrs != nil {
		errIdx := idx
		if errIdx >= len(f.listActiveErrs) {
			errIdx = len(f.listActiveErrs) - 1
		}
		if err := f.listActiveErrs[errIdx]; err != nil {
			return nil, err
		}
	}
	if f.listActiveErr != nil {
		return nil, f.listActiveErr
	}
	if f.listActiveResults != nil {
		if idx >= len(f.listActiveResults) {
			idx = len(f.listActiveResults) - 1
		}
		return f.listActiveResults[idx], nil
	}
	return f.listActiveResult, nil
}

func (f *fakeService) Recurtail(_ context.Context, req curtailment.RecurtailRequest) (*models.Event, error) {
	f.mu.Lock()
	f.recurtailCalls = append(f.recurtailCalls, req)
	res, err := f.recurtailResult, f.recurtailErr
	f.mu.Unlock()
	return res, err
}

func (f *fakeService) startCallsLen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.startCalls)
}

func (f *fakeService) startCallAt(i int) curtailment.StartRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startCalls[i]
}

func (f *fakeService) stopCallsLen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.stopCalls)
}

func (f *fakeService) stopCallAt(i int) curtailment.StopRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopCalls[i]
}

func (f *fakeService) listActiveCallsLen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.listActiveCalls)
}

func sampleSource() SourceConfig {
	return SourceConfig{
		ID:                      42,
		OrganizationID:          7,
		ServiceUserID:           99,
		SourceName:              "site-a",
		ContractedCurtailmentKw: 12500,
		StalenessThreshold:      240 * time.Second,
		MinCurtailedDuration:    600 * time.Second,
	}
}

func testSourceEvent(src SourceConfig, eventUUID uuid.UUID, state models.EventState) *models.Event {
	actorID := sourceActorIDFor(src)
	return &models.Event{
		EventUUID:     eventUUID,
		OrgID:         src.OrganizationID,
		SourceActorID: &actorID,
		State:         state,
	}
}

func TestDriver_Dispatch_OnToOff(t *testing.T) {
	t.Parallel()

	newUUID := uuid.New()
	svc := &fakeService{
		startResult: &curtailment.Plan{EventUUID: &newUUID},
	}
	d := NewDriver(svc)

	src := sampleSource()
	src.CurtailMode = string(models.ModeFixedKw)
	edgeAt := time.Date(2026, 5, 28, 11, 59, 30, 0, time.UTC)

	outcome, err := d.Dispatch(context.Background(), src, EdgeOnToOff, edgeAt)

	require.NoError(t, err)
	assert.Equal(t, newUUID, outcome.EventUUID)

	require.Len(t, svc.startCalls, 1)
	start := svc.startCalls[0]
	assert.Equal(t, int64(7), start.OrgID)
	assert.Equal(t, models.ScopeTypeWholeOrg, start.Scope.Type)
	assert.Equal(t, models.ModeFixedKw, start.Mode)
	assert.Equal(t, models.PriorityEmergency, start.Priority)
	assert.InDelta(t, 12500.0, start.TargetKW, 0.001)
	assert.InDelta(t, 625.0, start.ToleranceKW, 0.001) // 5% of contracted kW
	assert.True(t, start.AllowUnbounded)
	assert.True(t, start.CanUseAdminControls)
	assert.Equal(t, int32(600), start.MinCurtailedDurationSec)
	assert.Equal(t, int64(99), start.CreatedByUserID)
	require.NotNil(t, start.ExternalSource)
	assert.Equal(t, "site-a", *start.ExternalSource)
	require.NotNil(t, start.ExternalReference)
	assert.Equal(t, "site-a:"+itoa(edgeAt.Unix()), *start.ExternalReference)
	assert.Equal(t, models.SourceActorWebhook, start.SourceActorType)
	require.NotNil(t, start.SourceActorID)
	assert.Equal(t, "mqtt:site-a", *start.SourceActorID)
}

func TestDriver_Dispatch_OffSignal_RecurtailsRestoringSourceEvent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		direction EdgeDirection
	}{
		{"message OFF", EdgeOnToOff},
		{"watchdog OFF", EdgeWatchdogOff},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			eventUUID := uuid.New()
			svc := &fakeService{
				listActiveResult: []*models.Event{
					testSourceEvent(sampleSource(), eventUUID, models.EventStateRestoring),
				},
				recurtailResult: &models.Event{EventUUID: eventUUID, State: models.EventStatePending},
			}
			d := NewDriver(svc)

			outcome, err := d.Dispatch(context.Background(), sampleSource(), tc.direction, time.Now())

			require.NoError(t, err)
			assert.Equal(t, eventUUID, outcome.EventUUID)
			require.Len(t, svc.recurtailCalls, 1)
			assert.Equal(t, eventUUID, svc.recurtailCalls[0].EventUUID)
			assert.Empty(t, svc.startCalls, "OFF must re-curtail the restoring source event, not Start a competing event")
		})
	}
}

func TestDriver_Dispatch_OffSignal_ExistingActiveSourceEventIsAlreadyCurtailing(t *testing.T) {
	t.Parallel()

	eventUUID := uuid.New()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(sampleSource(), eventUUID, models.EventStateActive),
		},
	}
	d := NewDriver(svc)

	outcome, err := d.Dispatch(context.Background(), sampleSource(), EdgeOnToOff, time.Now())

	require.NoError(t, err)
	assert.Equal(t, eventUUID, outcome.EventUUID)
	assert.Empty(t, svc.startCalls, "an existing active source event already satisfies OFF")
	assert.Empty(t, svc.recurtailCalls)
}

// Empty mode follows the MQTT default: FULL_FLEET.
func TestDriver_Dispatch_OnToOff_FullFleet(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mode string
	}{
		{"default", ""},
		{"explicit", string(models.ModeFullFleet)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newUUID := uuid.New()
			svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
			d := NewDriver(svc)

			src := sampleSource()
			src.CurtailMode = tc.mode
			src.ContractedCurtailmentKw = 0

			_, err := d.Dispatch(context.Background(), src, EdgeOnToOff, time.Now())
			require.NoError(t, err)

			require.Len(t, svc.startCalls, 1)
			start := svc.startCalls[0]
			assert.Equal(t, models.ModeFullFleet, start.Mode)
			assert.Zero(t, start.TargetKW, "full_fleet carries no kW target")
			assert.Zero(t, start.ToleranceKW)
			assert.Equal(t, models.PriorityEmergency, start.Priority)
		})
	}
}

func TestDriver_Dispatch_FullFleetEmptyReportsNoop(t *testing.T) {
	t.Parallel()

	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	d := NewDriver(svc)

	src := sampleSource()
	src.CurtailMode = string(models.ModeFullFleet)
	src.ContractedCurtailmentKw = 0

	outcome, err := d.Dispatch(context.Background(), src, EdgeWatchdogOff, time.Now())

	require.NoError(t, err)
	assert.Equal(t, newUUID, outcome.EventUUID)
	assert.True(t, outcome.EmptyFullFleetNoop)
}

func TestDriver_Dispatch_WatchdogOff(t *testing.T) {
	t.Parallel()

	newUUID := uuid.New()
	svc := &fakeService{
		startResult: &curtailment.Plan{EventUUID: &newUUID},
	}
	d := NewDriver(svc)

	src := sampleSource()
	// Pick a timestamp mid-window so the quantization is observable.
	// 11:55:37 with a 240 s threshold should quantize down to 11:52:00.
	edgeAt := time.Date(2026, 5, 28, 11, 55, 37, 0, time.UTC)

	outcome, err := d.Dispatch(context.Background(), src, EdgeWatchdogOff, edgeAt)

	require.NoError(t, err)
	assert.Equal(t, newUUID, outcome.EventUUID)

	require.Len(t, svc.startCalls, 1)
	start := svc.startCalls[0]
	require.NotNil(t, start.ExternalReference)
	wantWindow := (edgeAt.Unix() / int64(src.StalenessThreshold/time.Second)) * int64(src.StalenessThreshold/time.Second)
	assert.Equal(t, "site-a:watchdog:"+itoa(wantWindow), *start.ExternalReference)
}

// Watchdog ticks in one stale window must share an idempotency key.
func TestDriver_Dispatch_WatchdogOff_QuantizesWithinWindow(t *testing.T) {
	t.Parallel()

	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	d := NewDriver(svc)

	src := sampleSource() // StalenessThreshold = 240 s
	tickA := time.Date(2026, 5, 28, 11, 52, 5, 0, time.UTC)
	tickB := tickA.Add(60 * time.Second)  // same 240 s window
	tickC := tickA.Add(300 * time.Second) // next window

	_, err := d.Dispatch(context.Background(), src, EdgeWatchdogOff, tickA)
	require.NoError(t, err)
	_, err = d.Dispatch(context.Background(), src, EdgeWatchdogOff, tickB)
	require.NoError(t, err)
	_, err = d.Dispatch(context.Background(), src, EdgeWatchdogOff, tickC)
	require.NoError(t, err)

	require.Len(t, svc.startCalls, 3)
	refA := *svc.startCalls[0].ExternalReference
	refB := *svc.startCalls[1].ExternalReference
	refC := *svc.startCalls[2].ExternalReference
	assert.Equal(t, refA, refB, "ticks in the same staleness window must share external_reference")
	assert.NotEqual(t, refA, refC, "ticks in different staleness windows must diverge")
}

// Same-second OFF bursts need distinct keys; redelivery of one edge reuses it.
func TestDriver_Dispatch_SameSecondOffEdges_DistinctReferences(t *testing.T) {
	t.Parallel()

	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	d := NewDriver(svc)

	src := sampleSource()
	edgeAt := time.Date(2026, 5, 28, 11, 0, 0, 0, time.UTC) // shared publisher second
	priorFirst := edgeAt.Add(-30 * time.Second)             // anchor before the first OFF
	priorSecond := edgeAt.Add(-6 * time.Second)             // the intervening ON's anchor

	_, err := d.Dispatch(context.Background(), src, EdgeOnToOff, edgeAt, priorFirst)
	require.NoError(t, err)
	_, err = d.Dispatch(context.Background(), src, EdgeOnToOff, edgeAt, priorSecond)
	require.NoError(t, err)
	// Redelivery of the second OFF: same edge + same prior anchor.
	_, err = d.Dispatch(context.Background(), src, EdgeOnToOff, edgeAt, priorSecond)
	require.NoError(t, err)

	require.Len(t, svc.startCalls, 3)
	refFirst := *svc.startCalls[0].ExternalReference
	refSecond := *svc.startCalls[1].ExternalReference
	refRedelivery := *svc.startCalls[2].ExternalReference
	assert.NotEqual(t, refFirst, refSecond,
		"same-second OFFs after different prior edges must not collide")
	assert.Equal(t, refSecond, refRedelivery,
		"a redelivery (same prior anchor) must reuse the reference for idempotency")
}

func TestDriver_Dispatch_ReplayUsesPersistedEventUUID(t *testing.T) {
	t.Parallel()

	replayUUID := uuid.New()
	svc := &fakeService{
		startResult: &curtailment.Plan{
			ReplayEvent: &models.Event{EventUUID: replayUUID},
		},
	}
	d := NewDriver(svc)

	outcome, err := d.Dispatch(context.Background(), sampleSource(), EdgeOnToOff, time.Now())

	require.NoError(t, err)
	assert.Equal(t, replayUUID, outcome.EventUUID)
}

func TestDriver_Dispatch_RestoringReplayRecurtails(t *testing.T) {
	t.Parallel()

	src := sampleSource()
	eventUUID := uuid.New()
	svc := &fakeService{
		startResult: &curtailment.Plan{
			ReplayEvent: testSourceEvent(src, eventUUID, models.EventStateRestoring),
		},
		recurtailResult: &models.Event{EventUUID: eventUUID, State: models.EventStatePending},
	}
	d := NewDriver(svc)

	outcome, err := d.Dispatch(context.Background(), src, EdgeOnToOff, time.Now())

	require.NoError(t, err)
	assert.Equal(t, eventUUID, outcome.EventUUID)
	require.Len(t, svc.startCalls, 1, "replay still comes from Start idempotency lookup")
	require.Len(t, svc.recurtailCalls, 1, "restoring replay must be re-curtailed before OFF settles")
	assert.Equal(t, eventUUID, svc.recurtailCalls[0].EventUUID)
	assert.Equal(t, src.OrganizationID, svc.recurtailCalls[0].OrgID)
}

func TestDriver_Dispatch_InsufficientLoadIsError(t *testing.T) {
	t.Parallel()

	svc := &fakeService{
		startResult: &curtailment.Plan{
			InsufficientLoadDetail: &modes.InsufficientLoadDetail{
				AvailableKW: 1000,
				RequestedKW: 12500,
			},
		},
	}
	d := NewDriver(svc)

	_, err := d.Dispatch(context.Background(), sampleSource(), EdgeOnToOff, time.Now())

	require.Error(t, err)
	var insufficient *StartInsufficientLoadError
	require.ErrorAs(t, err, &insufficient)
	assert.Contains(t, err.Error(), "insufficient load")
	require.NotNil(t, insufficient.Detail)
	assert.Equal(t, 1000.0, insufficient.Detail.AvailableKW)
	assert.Equal(t, 12500.0, insufficient.Detail.RequestedKW)
}

func TestDriver_Dispatch_OffToOn(t *testing.T) {
	t.Parallel()

	activeUUID := uuid.New()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(sampleSource(), activeUUID, models.EventStateActive),
		},
		stopResult: &models.Event{EventUUID: activeUUID},
	}
	d := NewDriver(svc)

	outcome, err := d.Dispatch(context.Background(), sampleSource(), EdgeOffToOn, time.Now())

	require.NoError(t, err)
	assert.Equal(t, activeUUID, outcome.EventUUID)

	require.Len(t, svc.listActiveCalls, 1)
	assert.Equal(t, int64(7), svc.listActiveCalls[0])

	require.Len(t, svc.stopCalls, 1)
	assert.Equal(t, int64(7), svc.stopCalls[0].OrgID)
	assert.Equal(t, activeUUID, svc.stopCalls[0].EventUUID)
}

func TestDriver_Dispatch_OffToOn_NoActiveEvent(t *testing.T) {
	t.Parallel()

	svc := &fakeService{
		listActiveResult: nil,
	}
	d := NewDriver(svc)

	_, err := d.Dispatch(context.Background(), sampleSource(), EdgeOffToOn, time.Now())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoActiveEvent))
	assert.Empty(t, svc.stopCalls)
}

// Foreign events must not be stopped by this source's ON edge.
func TestDriver_Dispatch_OffToOn_ForeignEvent_NotStopped(t *testing.T) {
	t.Parallel()

	foreign := "user:42"
	svc := &fakeService{listActiveResult: []*models.Event{{EventUUID: uuid.New(), SourceActorID: &foreign}}}
	d := NewDriver(svc)

	_, err := d.Dispatch(context.Background(), sampleSource(), EdgeOffToOn, time.Now())

	require.ErrorIs(t, err, ErrNoActiveEvent)
	assert.Empty(t, svc.stopCalls, "must not stop an event this source did not create")
}

func TestDriver_Dispatch_EdgeNoneIsNoOp(t *testing.T) {
	t.Parallel()

	svc := &fakeService{}
	d := NewDriver(svc)

	outcome, err := d.Dispatch(context.Background(), sampleSource(), EdgeNone, time.Now())

	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, outcome.EventUUID)
	assert.Empty(t, svc.startCalls)
	assert.Empty(t, svc.stopCalls)
	assert.Empty(t, svc.listActiveCalls)
}

// Device overlap is retryable, not a satisfied OFF.
func TestDriver_Dispatch_OnToOff_AlreadyExistsPropagates(t *testing.T) {
	t.Parallel()

	svc := &fakeService{startErr: fleeterror.NewAlreadyExistsError("a selected device is already in a non-terminal curtailment")}
	d := NewDriver(svc)

	_, err := d.Dispatch(context.Background(), sampleSource(), EdgeOnToOff, time.Now())

	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "AlreadyExists must propagate so the worker retries")
}

// ActiveSourceEvent must find this source among concurrent events.
func TestDriver_Dispatch_OffToOn_FindsSourceEventAmongConcurrent(t *testing.T) {
	t.Parallel()

	other := "mqtt:site-b"
	myUUID := uuid.New()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			{EventUUID: uuid.New(), SourceActorID: &other}, // another site's event
			testSourceEvent(sampleSource(), myUUID, models.EventStateActive),
		},
		stopResult: &models.Event{EventUUID: myUUID},
	}
	d := NewDriver(svc)

	outcome, err := d.Dispatch(context.Background(), sampleSource(), EdgeOffToOn, time.Now())

	require.NoError(t, err)
	assert.Equal(t, myUUID, outcome.EventUUID)
	require.Len(t, svc.stopCalls, 1)
	assert.Equal(t, myUUID, svc.stopCalls[0].EventUUID, "must stop this source's event, not another site's")
}

// device_list sources carry configured identifiers into the scope.
func TestDriver_Dispatch_OnToOff_DeviceListScope(t *testing.T) {
	t.Parallel()

	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	d := NewDriver(svc)

	src := sampleSource()
	src.ScopeType = "device_list"
	src.ScopeDeviceIdentifiers = []string{"miner-1", "miner-2"}

	_, err := d.Dispatch(context.Background(), src, EdgeOnToOff, time.Now())
	require.NoError(t, err)
	require.Len(t, svc.startCalls, 1)
	assert.Equal(t, models.ScopeTypeDeviceList, svc.startCalls[0].Scope.Type)
	assert.Equal(t, []string{"miner-1", "miner-2"}, svc.startCalls[0].Scope.DeviceIdentifiers)
}

// Empty device_list scope is rejected before Start.
func TestDriver_Dispatch_DeviceListScopeRequiresIdentifiers(t *testing.T) {
	t.Parallel()

	svc := &fakeService{}
	d := NewDriver(svc)

	src := sampleSource()
	src.ScopeType = "device_list" // no identifiers

	_, err := d.Dispatch(context.Background(), src, EdgeOnToOff, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device_list")
	assert.Empty(t, svc.startCalls, "an invalid scope must not reach Start")
}

func TestDriver_Dispatch_SiteScopeRejectedUntilCoreSupport(t *testing.T) {
	t.Parallel()

	svc := &fakeService{}
	d := NewDriver(svc)

	siteID := int64(42)
	src := sampleSource()
	src.ScopeType = "site"
	src.ScopeSiteID = &siteID

	_, err := d.Dispatch(context.Background(), src, EdgeOnToOff, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported scope type")
	assert.Empty(t, svc.startCalls, "site scope must not reach Start until the curtailment core supports it")
}

// ResumeSourceEvent uses Recurtail instead of Start.
func TestDriver_ResumeSourceEvent(t *testing.T) {
	t.Parallel()

	eventUUID := uuid.New()
	svc := &fakeService{recurtailResult: &models.Event{EventUUID: eventUUID, State: models.EventStatePending}}
	d := NewDriver(svc)

	err := d.ResumeSourceEvent(context.Background(), &models.Event{EventUUID: eventUUID, OrgID: 7})

	require.NoError(t, err)
	require.Len(t, svc.recurtailCalls, 1)
	assert.Equal(t, int64(7), svc.recurtailCalls[0].OrgID)
	assert.Equal(t, eventUUID, svc.recurtailCalls[0].EventUUID)
	assert.Empty(t, svc.startCalls, "resume must not Start a fresh event")
}

func TestDriver_ResumeSourceEvent_PropagatesError(t *testing.T) {
	t.Parallel()

	svc := &fakeService{recurtailErr: errors.New("svc down")}
	d := NewDriver(svc)

	err := d.ResumeSourceEvent(context.Background(), &models.Event{EventUUID: uuid.New(), OrgID: 7})
	require.Error(t, err)
}
