package curtailment

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
	"github.com/block/proto-fleet/server/internal/infrastructure/metrics"
)

type fakeSourcesLister struct {
	sources []mqttingest.SourceConfig
	err     error
}

func (f *fakeSourcesLister) ListEnabledSources(context.Context) ([]mqttingest.SourceConfig, error) {
	return f.sources, f.err
}

// blockingSourcesLister hangs until the tick's context expires, standing in
// for a stuck DB query.
type blockingSourcesLister struct{}

func (blockingSourcesLister) ListEnabledSources(ctx context.Context) ([]mqttingest.SourceConfig, error) {
	<-ctx.Done()
	return nil, fmt.Errorf("list enabled sources: %w", ctx.Err())
}

type fakeRuntime struct {
	statuses map[int64]mqttingest.RuntimeStatus
}

func (f *fakeRuntime) SourceRuntimeStatus(sourceID int64) mqttingest.RuntimeStatus {
	return f.statuses[sourceID]
}

type fakeActiveLister struct {
	active []*models.MQTTSourceActiveCurtailment
	err    error
}

func (f *fakeActiveLister) ListMQTTSourcesWithActiveCurtailment(context.Context) ([]*models.MQTTSourceActiveCurtailment, error) {
	return f.active, f.err
}

type recordedGauge struct {
	labels metrics.MQTTSourceLabels
	value  bool
}

type recordingEmitter struct {
	connected []recordedGauge
	active    []recordedGauge
}

func (r *recordingEmitter) EmitMQTTSourceConnected(_ context.Context, labels metrics.MQTTSourceLabels, connected bool) {
	r.connected = append(r.connected, recordedGauge{labels: labels, value: connected})
}

func (r *recordingEmitter) EmitMQTTCurtailmentActive(_ context.Context, labels metrics.MQTTSourceLabels, active bool) {
	r.active = append(r.active, recordedGauge{labels: labels, value: active})
}

func newTestAlertMetricsLoop(t *testing.T, cfg AlertMetricsConfig) *AlertMetricsLoop {
	t.Helper()
	loop, err := NewAlertMetricsLoop(cfg)
	require.NoError(t, err)
	return loop
}

func testSource(id, orgID int64, name string) mqttingest.SourceConfig {
	return mqttingest.SourceConfig{ID: id, OrganizationID: orgID, SourceName: name, Enabled: true}
}

func activeSource(id, orgID int64, name string) *models.MQTTSourceActiveCurtailment {
	return &models.MQTTSourceActiveCurtailment{SourceID: id, OrganizationID: orgID, SourceName: name}
}

func TestAlertMetricsTickEmitsConnectionState(t *testing.T) {
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources: &fakeSourcesLister{sources: []mqttingest.SourceConfig{
			testSource(1, 10, "maestro-a"),
			testSource(2, 20, "maestro-b"),
		}},
		Runtime: &fakeRuntime{statuses: map[int64]mqttingest.RuntimeStatus{
			1: {State: mqttingest.RuntimeStateRunning},
			2: {State: mqttingest.RuntimeStateError},
		}},
		ActiveCurtailment: &fakeActiveLister{},
		Emitter:           emitter,
	})

	loop.tick(context.Background())

	require.Len(t, emitter.connected, 2)
	require.Equal(t, metrics.MQTTSourceLabels{OrganizationID: "10", SourceName: "maestro-a"}, emitter.connected[0].labels)
	require.True(t, emitter.connected[0].value)
	require.Equal(t, metrics.MQTTSourceLabels{OrganizationID: "20", SourceName: "maestro-b"}, emitter.connected[1].labels)
	require.False(t, emitter.connected[1].value)
}

func TestAlertMetricsTickTreatsUnknownRuntimeAsDisconnected(t *testing.T) {
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources:           &fakeSourcesLister{sources: []mqttingest.SourceConfig{testSource(1, 10, "maestro")}},
		Runtime:           &fakeRuntime{},
		ActiveCurtailment: &fakeActiveLister{},
		Emitter:           emitter,
	})

	loop.tick(context.Background())

	require.Len(t, emitter.connected, 1)
	require.False(t, emitter.connected[0].value)
}

func TestAlertMetricsTickEmitsCurtailmentActive(t *testing.T) {
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources: &fakeSourcesLister{sources: []mqttingest.SourceConfig{
			testSource(1, 10, "curtailing"),
			testSource(2, 20, "idle"),
		}},
		Runtime: &fakeRuntime{},
		ActiveCurtailment: &fakeActiveLister{active: []*models.MQTTSourceActiveCurtailment{
			activeSource(1, 10, "curtailing"),
			nil,
		}},
		Emitter: emitter,
	})

	loop.tick(context.Background())

	require.Len(t, emitter.active, 2)
	require.True(t, emitter.active[0].value, "a source with a non-terminal automation event must read as curtailed")
	require.False(t, emitter.active[1].value, "a source without an active event must read as restored")
}

func TestAlertMetricsTickEmitsForDisabledSourceWithActiveEvent(t *testing.T) {
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		// Source 2 is disabled (absent from the enabled list) but its
		// curtailment event is still live.
		Sources: &fakeSourcesLister{sources: []mqttingest.SourceConfig{testSource(1, 10, "enabled")}},
		Runtime: &fakeRuntime{},
		ActiveCurtailment: &fakeActiveLister{active: []*models.MQTTSourceActiveCurtailment{
			activeSource(2, 20, "disabled-but-curtailed"),
		}},
		Emitter: emitter,
	})

	loop.tick(context.Background())

	require.Len(t, emitter.connected, 1, "connection gauge is only emitted for enabled sources")
	require.Len(t, emitter.active, 2)
	require.False(t, emitter.active[0].value, "enabled source without an event reads as restored")
	require.Equal(t, metrics.MQTTSourceLabels{OrganizationID: "20", SourceName: "disabled-but-curtailed"}, emitter.active[1].labels)
	require.True(t, emitter.active[1].value, "a disabled source with a live event must keep the alert firing")
}

func TestAlertMetricsTickClearsDisabledSourceAfterRestore(t *testing.T) {
	active := &fakeActiveLister{active: []*models.MQTTSourceActiveCurtailment{
		activeSource(2, 20, "disabled-but-curtailed"),
	}}
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources:           &fakeSourcesLister{},
		Runtime:           &fakeRuntime{},
		ActiveCurtailment: active,
		Emitter:           emitter,
	})

	loop.tick(context.Background())
	require.Len(t, emitter.active, 1)
	require.True(t, emitter.active[0].value)

	// Event reaches a terminal state: one clearing 0 with the same labels.
	active.active = nil
	loop.tick(context.Background())
	require.Len(t, emitter.active, 2)
	require.Equal(t, emitter.active[0].labels, emitter.active[1].labels)
	require.False(t, emitter.active[1].value)

	// Steady state afterwards: nothing more is emitted for that source.
	loop.tick(context.Background())
	require.Len(t, emitter.active, 2)
}

func TestAlertMetricsTickNoClearingSampleWhenSourceReenabled(t *testing.T) {
	sources := &fakeSourcesLister{}
	active := &fakeActiveLister{active: []*models.MQTTSourceActiveCurtailment{
		activeSource(1, 10, "maestro"),
	}}
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources:           sources,
		Runtime:           &fakeRuntime{},
		ActiveCurtailment: active,
		Emitter:           emitter,
	})

	loop.tick(context.Background())
	require.Len(t, emitter.active, 1)
	require.True(t, emitter.active[0].value)

	// Source re-enabled with the event still live: the enabled-path emission
	// must not be followed by a spurious clearing 0.
	sources.sources = []mqttingest.SourceConfig{testSource(1, 10, "maestro")}
	loop.tick(context.Background())
	require.Len(t, emitter.active, 2)
	require.True(t, emitter.active[1].value)
}

func TestAlertMetricsTickClearingSampleSurvivesLookupError(t *testing.T) {
	active := &fakeActiveLister{active: []*models.MQTTSourceActiveCurtailment{
		activeSource(2, 20, "disabled-but-curtailed"),
	}}
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources:           &fakeSourcesLister{},
		Runtime:           &fakeRuntime{},
		ActiveCurtailment: active,
		Emitter:           emitter,
	})

	loop.tick(context.Background())
	require.Len(t, emitter.active, 1)

	// A failed lookup must not drop the pending clearing state.
	active.err = errors.New("db down")
	loop.tick(context.Background())
	require.Len(t, emitter.active, 1)

	active.err = nil
	active.active = nil
	loop.tick(context.Background())
	require.Len(t, emitter.active, 2)
	require.False(t, emitter.active[1].value, "clearing 0 must land once the lookup recovers")
}

func TestAlertMetricsTickRetiresConnectedSeriesWhenSourceDisabled(t *testing.T) {
	sources := &fakeSourcesLister{sources: []mqttingest.SourceConfig{testSource(1, 10, "maestro")}}
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources:           sources,
		Runtime:           &fakeRuntime{statuses: map[int64]mqttingest.RuntimeStatus{1: {State: mqttingest.RuntimeStateError}}},
		ActiveCurtailment: &fakeActiveLister{},
		Emitter:           emitter,
	})

	loop.tick(context.Background())
	require.Len(t, emitter.connected, 1)
	require.False(t, emitter.connected[0].value, "source starts disconnected")

	// Operator disables the disconnected source: one non-alerting tombstone
	// resolves the critical alert instead of letting it fire for ~10 more min.
	sources.sources = nil
	loop.tick(context.Background())
	require.Len(t, emitter.connected, 2)
	require.Equal(t, emitter.connected[0].labels, emitter.connected[1].labels)
	require.True(t, emitter.connected[1].value)

	// Steady state afterwards: nothing more is emitted for that source.
	loop.tick(context.Background())
	require.Len(t, emitter.connected, 2)
}

func TestAlertMetricsTickRetiresRenamedSourceSeries(t *testing.T) {
	sources := &fakeSourcesLister{sources: []mqttingest.SourceConfig{testSource(1, 10, "old-name")}}
	active := &fakeActiveLister{active: []*models.MQTTSourceActiveCurtailment{
		activeSource(1, 10, "old-name"),
	}}
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources:           sources,
		Runtime:           &fakeRuntime{statuses: map[int64]mqttingest.RuntimeStatus{1: {State: mqttingest.RuntimeStateError}}},
		ActiveCurtailment: active,
		Emitter:           emitter,
	})

	loop.tick(context.Background())
	require.Len(t, emitter.connected, 1)
	require.Len(t, emitter.active, 1)

	sources.sources = []mqttingest.SourceConfig{testSource(1, 10, "new-name")}
	active.active = []*models.MQTTSourceActiveCurtailment{activeSource(1, 10, "new-name")}
	loop.tick(context.Background())

	oldLabels := metrics.MQTTSourceLabels{OrganizationID: "10", SourceName: "old-name"}
	newLabels := metrics.MQTTSourceLabels{OrganizationID: "10", SourceName: "new-name"}
	// Fresh emissions under the new name plus one non-alerting tombstone per
	// gauge under the old name, so neither alert keeps firing for a dead series.
	require.Equal(t, []recordedGauge{
		{labels: newLabels, value: false},
		{labels: oldLabels, value: true},
	}, emitter.connected[1:])
	require.Equal(t, []recordedGauge{
		{labels: newLabels, value: true},
		{labels: oldLabels, value: false},
	}, emitter.active[1:])

	// Steady state under the new name: no further tombstones.
	loop.tick(context.Background())
	require.Len(t, emitter.connected, 4)
	require.Len(t, emitter.active, 4)
}

func TestAlertMetricsTickSkipsActiveEmitOnLookupError(t *testing.T) {
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources:           &fakeSourcesLister{sources: []mqttingest.SourceConfig{testSource(1, 10, "maestro")}},
		Runtime:           &fakeRuntime{statuses: map[int64]mqttingest.RuntimeStatus{1: {State: mqttingest.RuntimeStateRunning}}},
		ActiveCurtailment: &fakeActiveLister{err: errors.New("db down")},
		Emitter:           emitter,
	})

	loop.tick(context.Background())

	require.Len(t, emitter.connected, 1, "connection gauge must still be emitted")
	require.Empty(t, emitter.active, "unverifiable curtailment state must not be emitted")
}

func TestAlertMetricsTickToleratesSourceListError(t *testing.T) {
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources:           &fakeSourcesLister{err: errors.New("db down")},
		Runtime:           &fakeRuntime{},
		ActiveCurtailment: &fakeActiveLister{},
		Emitter:           emitter,
	})

	loop.tick(context.Background())

	require.Empty(t, emitter.connected)
	require.Empty(t, emitter.active)
}

func TestAlertMetricsTickTimesOutHungQuery(t *testing.T) {
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources:           blockingSourcesLister{},
		Runtime:           &fakeRuntime{},
		ActiveCurtailment: &fakeActiveLister{},
		Emitter:           emitter,
		Interval:          10 * time.Millisecond,
	})

	done := make(chan struct{})
	go func() {
		loop.tick(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tick did not return: hung query is not bounded by the tick timeout")
	}
	require.Empty(t, emitter.connected)
	require.Empty(t, emitter.active)
}

func TestAlertMetricsLoopStartStop(t *testing.T) {
	emitter := &recordingEmitter{}
	loop := newTestAlertMetricsLoop(t, AlertMetricsConfig{
		Sources:           &fakeSourcesLister{sources: []mqttingest.SourceConfig{testSource(1, 10, "maestro")}},
		Runtime:           &fakeRuntime{},
		ActiveCurtailment: &fakeActiveLister{},
		Emitter:           emitter,
	})

	require.NoError(t, loop.Start(context.Background()))
	require.NoError(t, loop.Start(context.Background()), "second Start must be a no-op")
	loop.Stop()
	loop.Stop() // second Stop must be a no-op

	// The first tick runs synchronously before the ticker wait, so Stop
	// after Start guarantees at least one emission.
	require.NotEmpty(t, emitter.connected)
}

func TestNewAlertMetricsLoopValidatesDependencies(t *testing.T) {
	base := AlertMetricsConfig{
		Sources:           &fakeSourcesLister{},
		Runtime:           &fakeRuntime{},
		ActiveCurtailment: &fakeActiveLister{},
		Emitter:           &recordingEmitter{},
	}
	for name, mutate := range map[string]func(*AlertMetricsConfig){
		"sources": func(c *AlertMetricsConfig) { c.Sources = nil },
		"runtime": func(c *AlertMetricsConfig) { c.Runtime = nil },
		"active":  func(c *AlertMetricsConfig) { c.ActiveCurtailment = nil },
		"emitter": func(c *AlertMetricsConfig) { c.Emitter = nil },
	} {
		cfg := base
		mutate(&cfg)
		_, err := NewAlertMetricsLoop(cfg)
		require.Error(t, err, "missing %s must be rejected", name)
	}
}
