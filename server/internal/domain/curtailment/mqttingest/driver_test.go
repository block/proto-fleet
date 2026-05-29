package mqttingest

import (
	"context"
	"errors"
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
type fakeService struct {
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
	f.startCalls = append(f.startCalls, req)
	return f.startResult, f.startErr
}

func (f *fakeService) Stop(_ context.Context, req curtailment.StopRequest) (*models.Event, error) {
	f.stopCalls = append(f.stopCalls, req)
	return f.stopResult, f.stopErr
}

func (f *fakeService) GetActive(_ context.Context, orgID int64) (*models.Event, error) {
	f.getActiveCalls = append(f.getActiveCalls, orgID)
	return f.getActiveResult, f.getActiveErr
}

func sampleSource() SourceConfig {
	return SourceConfig{
		ID:                      42,
		OrganizationID:          7,
		ServiceUserID:           99,
		SourceName:              "site-a",
		ContractedCurtailmentKw: 12500,
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
	edgeAt := time.Date(2026, 5, 28, 11, 55, 0, 0, time.UTC)

	outcome, err := d.Dispatch(context.Background(), src, EdgeWatchdogOff, edgeAt)

	require.NoError(t, err)
	assert.Equal(t, EdgeWatchdogOff, outcome.Direction)
	assert.Equal(t, newUUID, outcome.EventUUID)

	require.Len(t, svc.startCalls, 1)
	start := svc.startCalls[0]
	require.NotNil(t, start.ExternalReference)
	assert.Equal(t, "site-a:watchdog:"+itoa(edgeAt.Unix()), *start.ExternalReference)
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
