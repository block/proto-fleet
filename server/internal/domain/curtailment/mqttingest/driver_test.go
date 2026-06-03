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

// fakeService captures Start/Stop/ListActive calls so tests can assert
// the driver's translation of edges into curtailment-service requests.
// Methods take the mutex so the subscriber test can safely poll
// startCalls from a different goroutine than the worker that drives
// dispatch.
type fakeService struct {
	mu              sync.Mutex
	startCalls      []curtailment.StartRequest
	stopCalls       []curtailment.StopRequest
	listActiveCalls []int64

	startResult      *curtailment.Plan
	startErr         error
	stopResult       *models.Event
	stopErr          error
	listActiveResult []*models.Event
	listActiveErr    error
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
	f.listActiveCalls = append(f.listActiveCalls, orgID)
	res, err := f.listActiveResult, f.listActiveErr
	f.mu.Unlock()
	return res, err
}

// startCallsLen is the lock-protected read the subscriber test uses
// to poll for dispatch completion.
func (f *fakeService) startCallsLen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.startCalls)
}

// startCallAt returns a copy of the i-th captured Start request.
func (f *fakeService) startCallAt(i int) curtailment.StartRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startCalls[i]
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

func TestDriver_Dispatch_OnToOff(t *testing.T) {
	t.Parallel()

	newUUID := uuid.New()
	svc := &fakeService{
		startResult: &curtailment.Plan{EventUUID: &newUUID},
	}
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	d := NewDriver(svc, func() time.Time { return now })

	src := sampleSource()
	edgeAt := time.Date(2026, 5, 28, 11, 59, 30, 0, time.UTC)

	outcome, err := d.Dispatch(context.Background(), src, EdgeOnToOff, edgeAt)

	require.NoError(t, err)
	assert.Equal(t, newUUID, outcome.EventUUID)
	assert.Equal(t, now, outcome.DispatchedAt)

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

func TestDriver_Dispatch_WatchdogOff(t *testing.T) {
	t.Parallel()

	newUUID := uuid.New()
	svc := &fakeService{
		startResult: &curtailment.Plan{EventUUID: &newUUID},
	}
	d := NewDriver(svc, nil)

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

// Back-to-back watchdog ticks in one staleness window must share an
// external_reference so the partial-unique index dedupes them as replays.
func TestDriver_Dispatch_WatchdogOff_QuantizesWithinWindow(t *testing.T) {
	t.Parallel()

	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	d := NewDriver(svc, nil)

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

func TestDriver_Dispatch_ReplayUsesPersistedEventUUID(t *testing.T) {
	t.Parallel()

	replayUUID := uuid.New()
	svc := &fakeService{
		startResult: &curtailment.Plan{
			ReplayEvent: &models.Event{EventUUID: replayUUID},
		},
	}
	d := NewDriver(svc, nil)

	outcome, err := d.Dispatch(context.Background(), sampleSource(), EdgeOnToOff, time.Now())

	require.NoError(t, err)
	assert.Equal(t, replayUUID, outcome.EventUUID)
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
	d := NewDriver(svc, nil)

	_, err := d.Dispatch(context.Background(), sampleSource(), EdgeOnToOff, time.Now())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient load")
}

func TestDriver_Dispatch_OffToOn(t *testing.T) {
	t.Parallel()

	activeUUID := uuid.New()
	actorID := "mqtt:site-a" // this source's own event (sampleSource is "site-a")
	svc := &fakeService{
		listActiveResult: []*models.Event{{EventUUID: activeUUID, SourceActorID: &actorID}},
		stopResult:       &models.Event{EventUUID: activeUUID},
	}
	d := NewDriver(svc, nil)

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
	d := NewDriver(svc, nil)

	_, err := d.Dispatch(context.Background(), sampleSource(), EdgeOffToOn, time.Now())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoActiveEvent))
	assert.Empty(t, svc.stopCalls)
}

// An active event owned by a different actor (a manual curtailment or another
// source) must not be stopped by this source's OFF→ON edge.
func TestDriver_Dispatch_OffToOn_ForeignEvent_NotStopped(t *testing.T) {
	t.Parallel()

	foreign := "user:42"
	svc := &fakeService{listActiveResult: []*models.Event{{EventUUID: uuid.New(), SourceActorID: &foreign}}}
	d := NewDriver(svc, nil)

	_, err := d.Dispatch(context.Background(), sampleSource(), EdgeOffToOn, time.Now())

	require.ErrorIs(t, err, ErrNoActiveEvent)
	assert.Empty(t, svc.stopCalls, "must not stop an event this source did not create")
}

func TestDriver_Dispatch_EdgeNoneIsNoOp(t *testing.T) {
	t.Parallel()

	svc := &fakeService{}
	d := NewDriver(svc, nil)

	outcome, err := d.Dispatch(context.Background(), sampleSource(), EdgeNone, time.Now())

	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, outcome.EventUUID)
	assert.Empty(t, svc.startCalls)
	assert.Empty(t, svc.stopCalls)
	assert.Empty(t, svc.listActiveCalls)
}

// A device-overlap AlreadyExists (a concurrent event grabbed one of this
// scope's devices) is a retryable dispatch error, not a satisfied OFF: each
// source curtails its own scope, so another event never satisfies it.
func TestDriver_Dispatch_OnToOff_AlreadyExistsPropagates(t *testing.T) {
	t.Parallel()

	svc := &fakeService{startErr: fleeterror.NewAlreadyExistsError("a selected device is already in a non-terminal curtailment")}
	d := NewDriver(svc, nil)

	_, err := d.Dispatch(context.Background(), sampleSource(), EdgeOnToOff, time.Now())

	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "AlreadyExists must propagate so the worker retries")
}

// With multiple concurrent events per org (one per disjoint scope),
// ActiveSourceEvent must find THIS source's event even when it isn't the
// most-recent, so OFF→ON stops the right one.
func TestDriver_Dispatch_OffToOn_FindsSourceEventAmongConcurrent(t *testing.T) {
	t.Parallel()

	other := "mqtt:site-b"
	mine := "mqtt:site-a" // sampleSource is "site-a"
	myUUID := uuid.New()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			{EventUUID: uuid.New(), SourceActorID: &other}, // another site's event
			{EventUUID: myUUID, SourceActorID: &mine},
		},
		stopResult: &models.Event{EventUUID: myUUID},
	}
	d := NewDriver(svc, nil)

	outcome, err := d.Dispatch(context.Background(), sampleSource(), EdgeOffToOn, time.Now())

	require.NoError(t, err)
	assert.Equal(t, myUUID, outcome.EventUUID)
	require.Len(t, svc.stopCalls, 1)
	assert.Equal(t, myUUID, svc.stopCalls[0].EventUUID, "must stop this source's event, not another site's")
}

// A device_list source dispatches a device_list scope carrying its configured
// identifiers (a "site" expressed as an explicit device list).
func TestDriver_Dispatch_OnToOff_DeviceListScope(t *testing.T) {
	t.Parallel()

	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	d := NewDriver(svc, nil)

	src := sampleSource()
	src.ScopeType = "device_list"
	src.ScopeDeviceIdentifiers = []string{"miner-1", "miner-2"}

	_, err := d.Dispatch(context.Background(), src, EdgeOnToOff, time.Now())
	require.NoError(t, err)
	require.Len(t, svc.startCalls, 1)
	assert.Equal(t, models.ScopeTypeDeviceList, svc.startCalls[0].Scope.Type)
	assert.Equal(t, []string{"miner-1", "miner-2"}, svc.startCalls[0].Scope.DeviceIdentifiers)
}

// A device_list source with no identifiers is a config error caught before Start.
func TestDriver_Dispatch_DeviceListScopeRequiresIdentifiers(t *testing.T) {
	t.Parallel()

	svc := &fakeService{}
	d := NewDriver(svc, nil)

	src := sampleSource()
	src.ScopeType = "device_list" // no identifiers

	_, err := d.Dispatch(context.Background(), src, EdgeOnToOff, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device_list")
	assert.Empty(t, svc.startCalls, "an invalid scope must not reach Start")
}
