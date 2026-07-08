package curtailment

import (
	"context"
	"errors"
	"testing"

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
