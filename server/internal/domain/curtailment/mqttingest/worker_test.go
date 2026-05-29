package mqttingest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
)

// newTestWorker wires a sourceWorker for direct-method exercise. The
// fake store and service let tests inspect persistence + dispatch.
func newTestWorker(t *testing.T, store *fakeStore, svc *fakeService, src SourceConfig) *sourceWorker {
	t.Helper()
	cfg := Config{
		Store:             store,
		Driver:            NewDriver(svc, nil),
		NewClient:         func() MQTTClient { return newFakeMQTTClient() },
		Decryptor:         passthroughDecryptor{},
		Logger:            slog.New(slog.DiscardHandler),
		Clock:             time.Now,
		WatchdogTickEvery: time.Second,
		BrokerFreshness:   60 * time.Second,
		ShutdownDeadline:  time.Second,
	}
	return &sourceWorker{
		cfg:           cfg,
		source:        src,
		primaryHost:   src.BrokerPrimaryHost,
		secondaryHost: src.BrokerSecondaryHost,
		lastObs:       map[BrokerRole]*Observation{},
	}
}

func workerSource() SourceConfig {
	return SourceConfig{
		ID:                      1,
		OrganizationID:          7,
		ServiceUserID:           99,
		SourceName:              "site-a",
		BrokerPrimaryHost:       "10.0.0.1",
		BrokerSecondaryHost:     "10.0.0.2",
		BrokerPort:              1883,
		ContractedCurtailmentKw: 12500,
		StalenessThreshold:      240 * time.Second,
		MinCurtailedDuration:    600 * time.Second,
		Enabled:                 true,
	}
}

// TestWorker_HandleWatchdog_PersistsTargetOff is the regression test
// for the earlier bug where handleWatchdog dispatched WATCHDOG_OFF but
// never advanced LastTarget — so EvaluateWatchdog kept returning Fire
// on every subsequent tick.
func TestWorker_HandleWatchdog_PersistsTargetOff(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())

	stale := time.Now().Add(-5 * time.Minute) // older than 240 s threshold
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastReceivedAt: stale,
	}

	next := w.handleWatchdog(context.Background(), prior)

	require.Equal(t, 1, svc.startCallsLen(), "watchdog must dispatch one Start")
	assert.Equal(t, TargetOff, next.LastTarget, "in-memory state must record OFF after watchdog dispatch")

	persisted, err := store.GetSourceState(context.Background(), w.source.ID)
	require.NoError(t, err)
	assert.Equal(t, TargetOff, persisted.LastTarget, "persisted state must record OFF so next tick is idle")
}

// TestWorker_HandleWatchdog_DispatchFailure_DoesNotAdvance proves the
// state is left untouched when the curtailment service rejects the
// Start; the next tick must retry against the same staleness window.
func TestWorker_HandleWatchdog_DispatchFailure_DoesNotAdvance(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{startErr: errors.New("svc down")}
	w := newTestWorker(t, store, svc, workerSource())

	stale := time.Now().Add(-5 * time.Minute)
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastReceivedAt: stale,
	}

	next := w.handleWatchdog(context.Background(), prior)

	assert.Equal(t, TargetOn, next.LastTarget, "failed dispatch must leave LastTarget unchanged")
	_, err := store.GetSourceState(context.Background(), w.source.ID)
	assert.ErrorIs(t, err, ErrSourceStateNotFound, "failed dispatch must not persist state")
}

// TestWorker_HandleMessage_DispatchFailure_KeepsLastTarget covers the
// other half of the gating fix: a failed Start must not advance
// LastTarget to the canonical wire target, otherwise the next identical
// observation would be treated as a no-op repeat and the site would
// silently uncurtail.
func TestWorker_HandleMessage_DispatchFailure_KeepsLastTarget(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{startErr: errors.New("svc down")}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	body, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
	}
	obs := observation{broker: w.primaryHost, payload: body, receivedAt: now}

	next := w.handleMessage(context.Background(), prior, obs)

	assert.Equal(t, TargetOn, next.LastTarget,
		"failed dispatch must not advance LastTarget — the implied edge did not actually run")
	assert.Equal(t, now, next.LastReceivedAt,
		"freshness must still advance — we heard a message, the dispatch is what failed")
	assert.Equal(t, w.primaryHost, next.LastReceivedBroker)
}
