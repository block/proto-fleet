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
)

// fakeService captures Start/Stop/GetActive calls so tests can assert
// the driver's translation of edges into curtailment-service requests.
// Methods take the mutex so the subscriber test can safely poll
// startCalls from a different goroutine than the worker that drives
// dispatch.
type fakeService struct {
	mu             sync.Mutex
	startCalls     []curtailment.StartRequest
	stopCalls      []curtailment.StopRequest
	getActiveCalls []int64

	startResult     *curtailment.Plan
	startErr        error
	stopResult      *models.Event
	stopErr         error
	getActiveResult *models.Event
	getActiveErr    error
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

func (f *fakeService) GetActive(_ context.Context, orgID int64) (*models.Event, error) {
	f.mu.Lock()
	f.getActiveCalls = append(f.getActiveCalls, orgID)
	res, err := f.getActiveResult, f.getActiveErr
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
	assert.Equal(t, EdgeOnToOff, outcome.Direction)
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
	assert.Equal(t, EdgeWatchdogOff, outcome.Direction)
	assert.Equal(t, newUUID, outcome.EventUUID)

	require.Len(t, svc.startCalls, 1)
	start := svc.startCalls[0]
	require.NotNil(t, start.ExternalReference)
	wantWindow := (edgeAt.Unix() / int64(src.StalenessThreshold/time.Second)) * int64(src.StalenessThreshold/time.Second)
	assert.Equal(t, "site-a:watchdog:"+itoa(wantWindow), *start.ExternalReference)
}

// Back-to-back watchdog ticks inside one staleness window must produce
// the same external_reference so the curtailment-service partial unique
// index dedupes them as replays. Without quantization, a 1 s ticker
// would generate a fresh reference every second and trigger a full
// selector pass per tick.
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
	svc := &fakeService{
		getActiveResult: &models.Event{EventUUID: activeUUID},
		stopResult:      &models.Event{EventUUID: activeUUID},
	}
	d := NewDriver(svc, nil)

	outcome, err := d.Dispatch(context.Background(), sampleSource(), EdgeOffToOn, time.Now())

	require.NoError(t, err)
	assert.Equal(t, EdgeOffToOn, outcome.Direction)
	assert.Equal(t, activeUUID, outcome.EventUUID)

	require.Len(t, svc.getActiveCalls, 1)
	assert.Equal(t, int64(7), svc.getActiveCalls[0])

	require.Len(t, svc.stopCalls, 1)
	assert.Equal(t, int64(7), svc.stopCalls[0].OrgID)
	assert.Equal(t, activeUUID, svc.stopCalls[0].EventUUID)
}

func TestDriver_Dispatch_OffToOn_NoActiveEvent(t *testing.T) {
	t.Parallel()

	svc := &fakeService{
		getActiveResult: nil,
	}
	d := NewDriver(svc, nil)

	_, err := d.Dispatch(context.Background(), sampleSource(), EdgeOffToOn, time.Now())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoActiveEvent))
	assert.Empty(t, svc.stopCalls)
}

func TestDriver_Dispatch_EdgeNoneIsNoOp(t *testing.T) {
	t.Parallel()

	svc := &fakeService{}
	d := NewDriver(svc, nil)

	outcome, err := d.Dispatch(context.Background(), sampleSource(), EdgeNone, time.Now())

	require.NoError(t, err)
	assert.Equal(t, EdgeNone, outcome.Direction)
	assert.Equal(t, uuid.Nil, outcome.EventUUID)
	assert.Empty(t, svc.startCalls)
	assert.Empty(t, svc.stopCalls)
	assert.Empty(t, svc.getActiveCalls)
}
