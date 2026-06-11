package mqttingest

import (
	"context"
	"fmt"
	"strings"
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
	createErr error
	updateErr error
}

func newFakeSettingsStore(configs ...SourceConfig) *fakeSettingsStore {
	store := &fakeSettingsStore{
		nextID:  1,
		configs: make(map[int64]SourceConfig),
		states:  make(map[int64]SourceState),
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
	reconcileErr       error
	sawCanceledContext bool
	contextValue       any
	status             RuntimeStatus
}

type fakeRuntimeContextKey struct{}

func (f *fakeRuntimeController) Reconcile(ctx context.Context) error {
	f.reconcileCalls++
	if ctx.Err() != nil {
		f.sawCanceledContext = true
	}
	f.contextValue = ctx.Value(fakeRuntimeContextKey{})
	return f.reconcileErr
}

func (f *fakeRuntimeController) SourceRuntimeStatus(int64) RuntimeStatus {
	return f.status
}

func (f *fakeRuntimeController) QuiesceSource(context.Context, int64) error {
	f.quiesceCalls++
	return nil
}

type fakeSourceConnectionTester struct {
	calls int
	req   TestSourceConnectionRequest
	out   TestSourceConnectionResult
	err   error
}

func (f *fakeSourceConnectionTester) TestConnection(_ context.Context, req TestSourceConnectionRequest) (TestSourceConnectionResult, error) {
	f.calls++
	f.req = req
	return f.out, f.err
}

func validSettingsSource() SourceConfig {
	return SourceConfig{
		OrganizationID:      42,
		ServiceUserID:       99,
		SourceName:          "maestro",
		Topic:               "maestro/curtailment",
		BrokerPrimaryHost:   "10.0.0.1",
		BrokerSecondaryHost: "10.0.0.2",
		BrokerPort:          1883,
		BrokerTransport:     "tcp",
		MQTTUsername:        "user",
		PayloadFormat:       "target_timestamp",
		StalenessThreshold:  240 * time.Second,
	}
}

func TestSettingsService_CreateDefaultsEnabledAndEncryptsPassword(t *testing.T) {
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

	assert.True(t, view.Config.Enabled)
	assert.Equal(t, "enc:secret", view.Config.MQTTPasswordEncrypted)
	assert.Equal(t, int32(1883), view.Config.BrokerPort)
	assert.Equal(t, 1, cipher.encryptCalls)
	assert.Equal(t, 1, runtime.reconcileCalls)
}

func TestSettingsService_TestConnectionNormalizesAndDelegatesWithoutPersistence(t *testing.T) {
	t.Parallel()

	tester := &fakeSourceConnectionTester{
		out: TestSourceConnectionResult{Results: []BrokerConnectionResult{{
			Broker:     "10.0.0.1",
			Role:       BrokerPrimary,
			Connected:  true,
			Subscribed: true,
		}}},
	}
	svc, err := NewSettingsService(SettingsServiceConfig{
		Store:            newFakeSettingsStore(),
		Cipher:           &fakeSettingsCipher{},
		ConnectionTester: tester,
	})
	require.NoError(t, err)

	source := validSettingsSource()
	source.SourceName = ""
	source.BrokerPort = 0
	source.BrokerTransport = ""
	source.PayloadFormat = ""
	source.StalenessThreshold = 0
	result, err := svc.TestConnection(t.Context(), TestSourceConnectionRequest{
		Source:            source,
		PlaintextPassword: "secret",
	})

	require.NoError(t, err)
	assert.True(t, result.OK())
	assert.Equal(t, 1, tester.calls)
	assert.Equal(t, connectionTestSourceName, tester.req.Source.SourceName)
	assert.Equal(t, defaultBrokerPort, tester.req.Source.BrokerPort)
	assert.Equal(t, brokerTransportTCP, tester.req.Source.BrokerTransport)
	assert.Equal(t, payloadFormatTargetTimestamp, tester.req.Source.PayloadFormat)
	assert.Equal(t, time.Duration(defaultStalenessThresholdSec)*time.Second, tester.req.Source.StalenessThreshold)
	assert.Equal(t, "secret", tester.req.PlaintextPassword)
}

func TestSettingsService_TestConnectionRejectsMissingPasswordBeforeBrokerCall(t *testing.T) {
	t.Parallel()

	tester := &fakeSourceConnectionTester{}
	svc, err := NewSettingsService(SettingsServiceConfig{
		Store:            newFakeSettingsStore(),
		Cipher:           &fakeSettingsCipher{},
		ConnectionTester: tester,
	})
	require.NoError(t, err)

	_, err = svc.TestConnection(t.Context(), TestSourceConnectionRequest{
		Source: validSettingsSource(),
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "mqtt_password is required")
	assert.Zero(t, tester.calls)
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

func TestSettingsService_CreateRejectsSourceNameLongerThanSchema(t *testing.T) {
	t.Parallel()

	svc, err := NewSettingsService(SettingsServiceConfig{Store: newFakeSettingsStore(), Cipher: &fakeSettingsCipher{}})
	require.NoError(t, err)

	source := validSettingsSource()
	source.SourceName = strings.Repeat("a", maxMQTTSourceNameLength+1)
	_, err = svc.Create(t.Context(), CreateSourceRequest{
		Source:            source,
		PlaintextPassword: "secret",
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "source_name must be at most 64 characters")
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

	ctx := context.WithValue(t.Context(), fakeRuntimeContextKey{}, "reload")
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = svc.SetEnabled(ctx, 42, 7, true)

	require.NoError(t, err)
	assert.Equal(t, 1, runtime.reconcileCalls)
	assert.False(t, runtime.sawCanceledContext, "reload must not inherit the client request cancellation")
	assert.Equal(t, "reload", runtime.contextValue, "reload should retain request context values for tracing/log correlation")
}

func TestSettingsService_DisableQuiescesRuntimeAndKeepsSourceState(t *testing.T) {
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
	runtime := &fakeRuntimeController{}
	svc, err := NewSettingsService(SettingsServiceConfig{Store: store, Cipher: &fakeSettingsCipher{}, Runtime: runtime})
	require.NoError(t, err)

	view, err := svc.SetEnabled(t.Context(), 42, 7, false)

	require.NoError(t, err)
	assert.False(t, view.Config.Enabled)
	assert.Equal(t, 1, runtime.quiesceCalls)
	assert.Equal(t, 1, runtime.reconcileCalls)
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

func TestSettingsService_DeleteDisabledSourceWithSignalState(t *testing.T) {
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

	require.NoError(t, err)
}
