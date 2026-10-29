package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type blockingDisableStore struct {
	*fakeSettingsStore
	states       *fakeSourceStore
	blockedID    int64
	writeStarted chan struct{}
	releaseWrite <-chan struct{}
	writeErr     error
	startedOnce  sync.Once
}

func (s *blockingDisableStore) ListEnabledSources(context.Context) ([]SourceConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var enabled []SourceConfig
	for _, source := range s.configs {
		if source.Enabled {
			enabled = append(enabled, source)
		}
	}
	return enabled, nil
}

func (s *blockingDisableStore) GetSourceState(ctx context.Context, sourceID int64) (SourceState, error) {
	return s.states.GetSourceState(ctx, sourceID)
}

func (s *blockingDisableStore) UpsertSourceState(ctx context.Context, update StateUpdate) error {
	return s.states.UpsertSourceState(ctx, update)
}

func (s *blockingDisableStore) SetSourceConfigEnabled(ctx context.Context, orgID, sourceID int64, enabled bool) (SourceConfig, error) {
	if sourceID == s.blockedID && !enabled {
		s.startedOnce.Do(func() { close(s.writeStarted) })
		select {
		case <-s.releaseWrite:
		case <-ctx.Done():
			return SourceConfig{}, fmt.Errorf("disable write canceled: %w", ctx.Err())
		}
		if s.writeErr != nil {
			return SourceConfig{}, s.writeErr
		}
	}
	return s.fakeSettingsStore.SetSourceConfigEnabled(ctx, orgID, sourceID, enabled)
}

func TestSettingsMutationsKeepSourceQuiescedAcrossConcurrentReloads(t *testing.T) {
	t.Parallel()

	for _, failDisable := range []bool{false, true} {
		t.Run(fmt.Sprintf("disable_fails_%v", failDisable), func(t *testing.T) {
			t.Parallel()
			first := validSettingsSource()
			first.ID, first.Enabled, first.MQTTPasswordEncrypted = 1, true, "enc:secret"
			second := first
			second.ID, second.SourceName = 2, "other-source"
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			store := &blockingDisableStore{
				fakeSettingsStore: newFakeSettingsStore(first, second), states: newFakeSourceStore(),
				blockedID: first.ID, writeStarted: make(chan struct{}), releaseWrite: release,
			}
			if failDisable {
				store.writeErr = errors.New("disable write failed")
			}
			reg := &clientRegistry{}
			subscriber := newReconcileTestSubscriber(t, store, reg)
			require.NoError(t, subscriber.Start(t.Context()))
			t.Cleanup(func() { require.NoError(t, subscriber.Stop(context.Background())) })
			t.Cleanup(unblock)
			requireRunningBrokers(t, subscriber, first.ID, 2)
			requireRunningBrokers(t, subscriber, second.ID, 2)
			settings, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}, Runtime: subscriber})
			require.NoError(t, err)
			disableDone := make(chan error, 1)
			go func() {
				_, err := settings.SetEnabled(t.Context(), first.OrganizationID, first.ID, false)
				disableDone <- err
			}()
			<-store.writeStarted // A is quiesced, but its DB row is still enabled.

			// B's completed settings write must not release A's in-flight pause.
			_, err = settings.SetEnabled(t.Context(), second.OrganizationID, second.ID, true)
			require.NoError(t, err)
			require.NoError(t, subscriber.Reconcile(t.Context()), "an unscoped reload must also preserve A's pause")
			assert.Equal(t, RuntimeStateStopped, subscriber.SourceRuntimeStatus(first.ID).State)
			active, _, connects := reg.snapshot()
			assert.Equal(t, 2, active)
			assert.Equal(t, 4, connects)

			// A concurrent writer to A itself must wait through the complete
			// quiesce/write/reload interval rather than releasing the same pause.
			enableDone := make(chan error, 1)
			go func() {
				_, err := settings.SetEnabled(t.Context(), first.OrganizationID, first.ID, true)
				enableDone <- err
			}()
			key := sourceMutationKey{organizationID: first.OrganizationID, sourceID: first.ID}
			require.Eventually(t, func() bool {
				settings.sourceMutationMu.Lock()
				defer settings.sourceMutationMu.Unlock()
				return settings.sourceMutations[key] != nil && settings.sourceMutations[key].refs == 2
			}, time.Second, time.Millisecond)
			canceledCtx, cancel := context.WithCancel(t.Context())
			cancel()
			_, err = settings.Update(canceledCtx, UpdateSourceRequest{OrganizationID: first.OrganizationID, SourceID: first.ID})
			require.ErrorIs(t, err, context.Canceled, "same-source lock waits must respect cancellation")
			assert.Equal(t, RuntimeStateStopped, subscriber.SourceRuntimeStatus(first.ID).State)

			unblock()
			if failDisable {
				require.ErrorContains(t, <-disableDone, "disable write failed")
			} else {
				require.NoError(t, <-disableDone)
			}
			require.NoError(t, <-enableDone)
			requireRunningBrokers(t, subscriber, first.ID, 2)
			_, _, connects = reg.snapshot()
			assert.Equal(t, 6, connects, "A resumes once after commit or rollback; B does not restart")
			settings.sourceMutationMu.Lock()
			assert.Empty(t, settings.sourceMutations, "completed/canceled mutations must release lock entries")
			settings.sourceMutationMu.Unlock()
		})
	}
}
