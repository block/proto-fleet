package mqttingest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type fakeSettingsStore struct {
	mu        sync.Mutex
	nextID    int64
	configs   map[int64]SourceConfig
	states    map[int64]SourceState
	canIngest bool
	createErr error
	updateErr error
}

func newFakeSettingsStore(configs ...SourceConfig) *fakeSettingsStore {
	store := &fakeSettingsStore{
		nextID:    1,
		configs:   make(map[int64]SourceConfig),
		states:    make(map[int64]SourceState),
		canIngest: true,
	}
	for _, cfg := range configs {
		if cfg.ID == 0 {
			cfg.ID = store.nextID
			store.nextID++
		}
		store.configs[cfg.ID] = cfg
		if cfg.ID >= store.nextID {
			store.nextID = cfg.ID + 1
		}
	}
	return store
}

func (f *fakeSettingsStore) ListSourceConfigsByOrg(_ context.Context, orgID int64) ([]SourceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]SourceConfig, 0)
	for _, cfg := range f.configs {
		if cfg.OrganizationID == orgID {
			out = append(out, cfg)
		}
	}
	return out, nil
}

func (f *fakeSettingsStore) ListSourceStatesByOrg(_ context.Context, orgID int64) ([]SourceState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]SourceState, 0)
	for sourceID, state := range f.states {
		cfg, ok := f.configs[sourceID]
		if ok && cfg.OrganizationID == orgID {
			out = append(out, state)
		}
	}
	return out, nil
}

func (f *fakeSettingsStore) GetSourceConfigByOrg(_ context.Context, orgID, sourceID int64) (SourceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, ok := f.configs[sourceID]
	if !ok || cfg.OrganizationID != orgID {
		return SourceConfig{}, ErrSourceConfigNotFound
	}
	return cfg, nil
}

func (f *fakeSettingsStore) CreateSourceConfig(_ context.Context, source SourceConfig) (SourceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return SourceConfig{}, f.createErr
	}
	source.ID = f.nextID
	f.nextID++
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	source.CreatedAt = now
	source.UpdatedAt = now
	f.configs[source.ID] = source
	return source, nil
}

func (f *fakeSettingsStore) UpdateSourceConfig(_ context.Context, source SourceConfig) (SourceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return SourceConfig{}, f.updateErr
	}
	current, ok := f.configs[source.ID]
	if !ok || current.OrganizationID != source.OrganizationID {
		return SourceConfig{}, ErrSourceConfigNotFound
	}
	source.Enabled = current.Enabled
	source.CreatedAt = current.CreatedAt
	source.UpdatedAt = current.UpdatedAt.Add(time.Second)
	f.configs[source.ID] = source
	return source, nil
}

func (f *fakeSettingsStore) SetSourceConfigEnabled(_ context.Context, orgID, sourceID int64, enabled bool) (SourceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, ok := f.configs[sourceID]
	if !ok || cfg.OrganizationID != orgID {
		return SourceConfig{}, ErrSourceConfigNotFound
	}
	cfg.Enabled = enabled
	cfg.UpdatedAt = cfg.UpdatedAt.Add(time.Second)
	f.configs[sourceID] = cfg
	return cfg, nil
}

func (f *fakeSettingsStore) DeleteDisabledSourceConfig(_ context.Context, orgID, sourceID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, ok := f.configs[sourceID]
	if !ok || cfg.OrganizationID != orgID {
		return ErrSourceConfigNotFound
	}
	if cfg.Enabled {
		return ErrSourceConfigDeleteBlocked
	}
	delete(f.configs, sourceID)
	delete(f.states, sourceID)
	return nil
}

func (f *fakeSettingsStore) UserCanIngestCurtailment(_ context.Context, _, _ int64) (bool, error) {
	return f.canIngest, nil
}

type fakeSettingsCipher struct {
	encryptCalls int
}

func (f *fakeSettingsCipher) Encrypt(plaintext []byte) (string, error) {
	f.encryptCalls++
	return "enc:" + string(plaintext), nil
}

func (f *fakeSettingsCipher) Decrypt(encrypted string) ([]byte, error) {
	if len(encrypted) < 4 || encrypted[:4] != "enc:" {
		return nil, fmt.Errorf("unexpected ciphertext")
	}
	return []byte(encrypted[4:]), nil
}

type fakeRuntimeController struct {
	reconcileCalls     int
	quiesceCalls       int
	activeCurtailment  bool
	activeResults      []bool
	activeErr          error
	reconcileErr       error
	sawCanceledContext bool
	status             RuntimeStatus
}

func (f *fakeRuntimeController) Reconcile(ctx context.Context) error {
	f.reconcileCalls++
	if ctx.Err() != nil {
		f.sawCanceledContext = true
	}
	return f.reconcileErr
}

func (f *fakeRuntimeController) SourceRuntimeStatus(int64) RuntimeStatus {
	return f.status
}

func (f *fakeRuntimeController) QuiesceSource(context.Context, int64) error {
	f.quiesceCalls++
	return nil
}

func (f *fakeRuntimeController) SourceHasActiveCurtailment(context.Context, SourceConfig) (bool, error) {
	if f.activeErr != nil {
		return false, f.activeErr
	}
	if len(f.activeResults) > 0 {
		result := f.activeResults[0]
		f.activeResults = f.activeResults[1:]
		return result, nil
	}
	return f.activeCurtailment, nil
}

func validSettingsSource() SourceConfig {
	return SourceConfig{
		OrganizationID:       42,
		ServiceUserID:        99,
		SourceName:           "maestro",
		Topic:                "maestro/curtailment",
		BrokerPrimaryHost:    "10.0.0.1",
		BrokerSecondaryHost:  "10.0.0.2",
		BrokerPort:           1883,
		BrokerTransport:      "tcp",
		MQTTUsername:         "user",
		CurtailMode:          "FULL_FLEET",
		PayloadFormat:        "target_timestamp",
		ScopeType:            "whole_org",
		StalenessThreshold:   240 * time.Second,
		MinCurtailedDuration: 600 * time.Second,
	}
}

func TestSettingsService_CreateDefaultsDisabledAndEncryptsPassword(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore()
	cipher := &fakeSettingsCipher{}
	runtime := &fakeRuntimeController{}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: cipher, Runtime: runtime})
	require.NoError(t, err)

	view, err := svc.Create(t.Context(), CreateSourceRequest{
		Source:            validSettingsSource(),
		PlaintextPassword: "secret",
	})
	require.NoError(t, err)

	assert.False(t, view.Config.Enabled)
	assert.Equal(t, "enc:secret", view.Config.MQTTPasswordEncrypted)
	assert.Equal(t, int32(1883), view.Config.BrokerPort)
	assert.Equal(t, 1, cipher.encryptCalls)
	assert.Equal(t, 1, runtime.reconcileCalls)
}

func TestSettingsService_CreateDuplicateNameReturnsAlreadyExists(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore()
	store.createErr = ErrSourceConfigNameExists
	cipher := &fakeSettingsCipher{}
	runtime := &fakeRuntimeController{}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: cipher, Runtime: runtime})
	require.NoError(t, err)

	_, err = svc.Create(t.Context(), CreateSourceRequest{
		Source:            validSettingsSource(),
		PlaintextPassword: "secret",
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err))
	assert.Zero(t, runtime.reconcileCalls, "duplicate-name writes must not trigger runtime reload")
}

func TestSourceConfigPersistErrorMapsDuplicateNameConstraint(t *testing.T) {
	t.Parallel()

	err := sourceConfigPersistError("insert mqtt source config", &pgconn.PgError{
		Code:           db.PGUniqueViolation,
		ConstraintName: mqttSourceConfigOrgNameConstraint,
	})

	assert.ErrorIs(t, err, ErrSourceConfigNameExists)
}

func TestSourceConfigPersistErrorDoesNotMapOtherUniqueConstraints(t *testing.T) {
	t.Parallel()

	err := sourceConfigPersistError("insert mqtt source config", &pgconn.PgError{
		Code:           db.PGUniqueViolation,
		ConstraintName: "curtailment_mqtt_source_config_pkey",
	})

	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrSourceConfigNameExists)
}

func TestSettingsService_UpdatePreservesPasswordWhenOmittedAndReloadsRuntime(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.MQTTPasswordEncrypted = "enc:old"
	store := newFakeSettingsStore(source)
	cipher := &fakeSettingsCipher{}
	runtime := &fakeRuntimeController{}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: cipher, Runtime: runtime})
	require.NoError(t, err)

	nextTopic := "maestro/target"
	view, err := svc.Update(t.Context(), UpdateSourceRequest{
		OrganizationID: 42,
		SourceID:       7,
		Topic:          &nextTopic,
	})
	require.NoError(t, err)

	assert.Equal(t, nextTopic, view.Config.Topic)
	assert.Equal(t, "enc:old", view.Config.MQTTPasswordEncrypted)
	assert.Zero(t, cipher.encryptCalls)
	assert.Equal(t, 1, runtime.reconcileCalls)
}

func TestSettingsService_UpdateRequiresPasswordWhenBrokerBindingChanges(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.MQTTPasswordEncrypted = "enc:old"
	store := newFakeSettingsStore(source)
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}, Runtime: &fakeRuntimeController{}})
	require.NoError(t, err)

	nextHost := "10.0.0.3"
	_, err = svc.Update(t.Context(), UpdateSourceRequest{
		OrganizationID:    42,
		SourceID:          7,
		BrokerPrimaryHost: &nextHost,
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "mqtt_password is required")
}

func TestSettingsService_UpdateRotatesPasswordWhenBrokerBindingChanges(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.MQTTPasswordEncrypted = "enc:old"
	store := newFakeSettingsStore(source)
	cipher := &fakeSettingsCipher{}
	runtime := &fakeRuntimeController{}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: cipher, Runtime: runtime})
	require.NoError(t, err)

	nextHost := "10.0.0.3"
	nextPassword := "rotated"
	view, err := svc.Update(t.Context(), UpdateSourceRequest{
		OrganizationID:    42,
		SourceID:          7,
		BrokerPrimaryHost: &nextHost,
		PlaintextPassword: &nextPassword,
	})

	require.NoError(t, err)
	assert.Equal(t, nextHost, view.Config.BrokerPrimaryHost)
	assert.Equal(t, "enc:rotated", view.Config.MQTTPasswordEncrypted)
	assert.Equal(t, 1, cipher.encryptCalls)
	assert.Equal(t, 1, runtime.reconcileCalls)
}

func TestSettingsService_SetEnabledReloadUsesInternalContext(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.MQTTPasswordEncrypted = "enc:secret"
	store := newFakeSettingsStore(source)
	runtime := &fakeRuntimeController{}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}, Runtime: runtime})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = svc.SetEnabled(ctx, 42, 7, true)

	require.NoError(t, err)
	assert.Equal(t, 1, runtime.reconcileCalls)
	assert.False(t, runtime.sawCanceledContext, "reload must not inherit the client request cancellation")
}

func TestSettingsService_EnableRejectsSiteScopeUntilSiteSupportLands(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.ScopeType = "site"
	siteID := int64(123)
	source.ScopeSiteID = &siteID
	source.MQTTPasswordEncrypted = "enc:secret"
	store := newFakeSettingsStore(source)
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}})
	require.NoError(t, err)

	_, err = svc.SetEnabled(t.Context(), 42, 7, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "site-scoped MQTT")
}

func TestSettingsService_DisableRejectsActiveCurtailmentState(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.Enabled = true
	source.MQTTPasswordEncrypted = "enc:secret"
	store := newFakeSettingsStore(source)
	store.states[source.ID] = SourceState{SourceConfigID: source.ID, LastTarget: TargetOff}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}})
	require.NoError(t, err)

	_, err = svc.SetEnabled(t.Context(), 42, 7, false)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "restore MQTT source to ON")
}

func TestSettingsService_DisableRejectsPendingOff(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.Enabled = true
	source.MQTTPasswordEncrypted = "enc:secret"
	store := newFakeSettingsStore(source)
	store.states[source.ID] = SourceState{
		SourceConfigID: source.ID,
		LastTarget:     TargetOn,
		PendingEdge:    &PendingEdge{Direction: EdgeOnToOff, Target: TargetOff},
	}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}, Runtime: &fakeRuntimeController{}})
	require.NoError(t, err)

	_, err = svc.SetEnabled(t.Context(), 42, 7, false)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "restore MQTT source to ON")
}

func TestSettingsService_DisableRejectsActiveCurtailmentEvent(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.Enabled = true
	source.MQTTPasswordEncrypted = "enc:secret"
	store := newFakeSettingsStore(source)
	runtime := &fakeRuntimeController{activeCurtailment: true}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}, Runtime: runtime})
	require.NoError(t, err)

	_, err = svc.SetEnabled(t.Context(), 42, 7, false)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "restore MQTT source to ON")
	assert.Zero(t, runtime.quiesceCalls)
}

func TestSettingsService_DisableRejectsActiveCurtailmentAfterQuiesce(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.Enabled = true
	source.MQTTPasswordEncrypted = "enc:secret"
	store := newFakeSettingsStore(source)
	runtime := &fakeRuntimeController{activeResults: []bool{false, true}}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}, Runtime: runtime})
	require.NoError(t, err)

	_, err = svc.SetEnabled(t.Context(), 42, 7, false)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Equal(t, 1, runtime.quiesceCalls)
	assert.Equal(t, 1, runtime.reconcileCalls, "failed disable must restore the still-enabled runtime")
}

func TestSettingsService_DeleteRejectsEnabledSource(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.Enabled = true
	source.MQTTPasswordEncrypted = "enc:secret"
	store := newFakeSettingsStore(source)
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}})
	require.NoError(t, err)

	err = svc.Delete(t.Context(), 42, 7)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disable the MQTT source")
}

func TestSettingsService_DeleteRejectsActiveCurtailmentState(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.Enabled = false
	source.MQTTPasswordEncrypted = "enc:secret"
	store := newFakeSettingsStore(source)
	store.states[source.ID] = SourceState{SourceConfigID: source.ID, LastTarget: TargetOff}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}})
	require.NoError(t, err)

	err = svc.Delete(t.Context(), 42, 7)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "restore MQTT source to ON")
}

func TestSettingsService_DeleteRejectsPendingOff(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.Enabled = false
	source.MQTTPasswordEncrypted = "enc:secret"
	store := newFakeSettingsStore(source)
	store.states[source.ID] = SourceState{
		SourceConfigID: source.ID,
		LastTarget:     TargetOn,
		PendingEdge:    &PendingEdge{Direction: EdgeOnToOff, Target: TargetOff},
	}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}, Runtime: &fakeRuntimeController{}})
	require.NoError(t, err)

	err = svc.Delete(t.Context(), 42, 7)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "restore MQTT source to ON")
}

func TestSettingsService_DeleteRejectsActiveCurtailmentEvent(t *testing.T) {
	t.Parallel()

	source := validSettingsSource()
	source.ID = 7
	source.Enabled = false
	source.MQTTPasswordEncrypted = "enc:secret"
	store := newFakeSettingsStore(source)
	runtime := &fakeRuntimeController{activeCurtailment: true}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}, Runtime: runtime})
	require.NoError(t, err)

	err = svc.Delete(t.Context(), 42, 7)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "restore MQTT source to ON")
}
