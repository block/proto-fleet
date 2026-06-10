package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const maxMQTTSourceStringLength = 255
const settingsReconcileTimeout = 30 * time.Second

// PasswordCipher wraps and unwraps MQTT credentials.
type PasswordCipher interface {
	Encrypt(toEncrypt []byte) (string, error)
	Decrypt(toDecrypt string) ([]byte, error)
}

// RuntimeState is the in-process lifecycle state for a configured source.
type RuntimeState int

const (
	RuntimeStateUnspecified RuntimeState = iota
	RuntimeStateDisabled
	RuntimeStateStopped
	RuntimeStateStarting
	RuntimeStateRunning
	RuntimeStateError
)

// RuntimeStatus is an in-memory health snapshot. Durable signal state stays in
// SourceState so disabling or restarting fleetd does not discard pending edges.
type RuntimeStatus struct {
	State                 RuntimeState
	LastError             string
	RunningBrokerCount    int
	SubscribedBrokerCount int
	UpdatedAt             time.Time
}

// RuntimeController hot-reloads the subscriber after a settings write.
type RuntimeController interface {
	Reconcile(ctx context.Context) error
	QuiesceSource(ctx context.Context, sourceID int64) error
	SourceRuntimeStatus(sourceID int64) RuntimeStatus
}

// SettingsService validates, persists, redacts, and reloads MQTT sources.
type SettingsService struct {
	store            SettingsStore
	cipher           PasswordCipher
	runtime          RuntimeController
	clock            func() time.Time
	reconcileTimeout time.Duration
}

type SettingsServiceConfig struct {
	Store   SettingsStore
	Cipher  PasswordCipher
	Runtime RuntimeController
	Clock   func() time.Time
}

func NewSettingsService(cfg SettingsServiceConfig) (*SettingsService, error) {
	if cfg.Store == nil {
		return nil, errors.New("mqttingest: SettingsStore is required")
	}
	if cfg.Cipher == nil {
		return nil, errors.New("mqttingest: PasswordCipher is required")
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &SettingsService{
		store:            cfg.Store,
		cipher:           cfg.Cipher,
		runtime:          cfg.Runtime,
		clock:            cfg.Clock,
		reconcileTimeout: settingsReconcileTimeout,
	}, nil
}

type SourceView struct {
	Config   SourceConfig
	State    SourceState
	HasState bool
	Runtime  RuntimeStatus
	Stale    bool
}

type CreateSourceRequest struct {
	Source            SourceConfig
	PlaintextPassword string
}

type UpdateSourceRequest struct {
	OrganizationID int64
	SourceID       int64

	SourceName          *string
	Topic               *string
	BrokerPrimaryHost   *string
	BrokerSecondaryHost *string
	BrokerPort          *int32
	BrokerTransport     *string
	MQTTUsername        *string
	PlaintextPassword   *string
	PayloadFormat       *string
	StalenessThreshold  *time.Duration
	ClearStaleness      bool
}

func (s *SettingsService) List(ctx context.Context, orgID int64) ([]SourceView, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	configs, err := s.store.ListSourceConfigsByOrg(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list mqtt source settings: %w", err)
	}
	states, err := s.store.ListSourceStatesByOrg(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list mqtt source states: %w", err)
	}
	stateBySource := make(map[int64]SourceState, len(states))
	for _, state := range states {
		stateBySource[state.SourceConfigID] = state
	}
	views := make([]SourceView, len(configs))
	for i, cfg := range configs {
		state, ok := stateBySource[cfg.ID]
		views[i] = s.viewFor(cfg, state, ok)
	}
	return views, nil
}

func (s *SettingsService) Get(ctx context.Context, orgID, sourceID int64) (SourceView, error) {
	cfg, err := s.getConfig(ctx, orgID, sourceID)
	if err != nil {
		return SourceView{}, err
	}
	state, hasState, err := s.getStateForSource(ctx, orgID, sourceID)
	if err != nil {
		return SourceView{}, err
	}
	return s.viewFor(cfg, state, hasState), nil
}

func (s *SettingsService) Create(ctx context.Context, req CreateSourceRequest) (SourceView, error) {
	source := normalizeSourceConfig(req.Source)
	source.Enabled = true
	if source.OrganizationID <= 0 {
		return SourceView{}, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if strings.TrimSpace(req.PlaintextPassword) == "" {
		return SourceView{}, fleeterror.NewInvalidArgumentError("mqtt_password is required")
	}
	encrypted, err := s.encryptPassword(req.PlaintextPassword)
	if err != nil {
		return SourceView{}, err
	}
	source.MQTTPasswordEncrypted = encrypted
	if err := s.validateSourceConfig(ctx, source); err != nil {
		return SourceView{}, err
	}

	created, err := s.store.CreateSourceConfig(ctx, source)
	if err != nil {
		return SourceView{}, sourceStoreError("create mqtt source setting", err)
	}
	if err := s.reconcile(ctx); err != nil {
		return SourceView{}, err
	}
	state, hasState, err := s.getStateForSource(ctx, created.OrganizationID, created.ID)
	if err != nil {
		return SourceView{}, err
	}
	return s.viewFor(created, state, hasState), nil
}

func (s *SettingsService) Update(ctx context.Context, req UpdateSourceRequest) (SourceView, error) {
	if req.ClearStaleness && req.StalenessThreshold != nil {
		return SourceView{}, fleeterror.NewInvalidArgumentError("clear_staleness_threshold_sec conflicts with staleness_threshold_sec")
	}
	current, err := s.getConfig(ctx, req.OrganizationID, req.SourceID)
	if err != nil {
		return SourceView{}, err
	}
	next := current
	applyString(req.SourceName, &next.SourceName)
	applyString(req.Topic, &next.Topic)
	applyString(req.BrokerPrimaryHost, &next.BrokerPrimaryHost)
	applyString(req.BrokerSecondaryHost, &next.BrokerSecondaryHost)
	applyInt32(req.BrokerPort, &next.BrokerPort)
	applyString(req.BrokerTransport, &next.BrokerTransport)
	applyString(req.MQTTUsername, &next.MQTTUsername)
	applyString(req.PayloadFormat, &next.PayloadFormat)
	if req.ClearStaleness {
		next.StalenessThreshold = 0
	}
	if req.StalenessThreshold != nil {
		next.StalenessThreshold = *req.StalenessThreshold
	}

	next = normalizeSourceConfig(next)
	if req.PlaintextPassword != nil {
		if strings.TrimSpace(*req.PlaintextPassword) == "" {
			return SourceView{}, fleeterror.NewInvalidArgumentError("mqtt_password cannot be empty when set")
		}
		encrypted, err := s.encryptPassword(*req.PlaintextPassword)
		if err != nil {
			return SourceView{}, err
		}
		next.MQTTPasswordEncrypted = encrypted
	} else if mqttCredentialBindingChanged(current, next) {
		return SourceView{}, fleeterror.NewInvalidArgumentError(
			"mqtt_password is required when broker host, broker port, broker transport, or mqtt_username changes",
		)
	}
	if err := s.validateSourceConfig(ctx, next); err != nil {
		return SourceView{}, err
	}
	updated, err := s.store.UpdateSourceConfig(ctx, next)
	if err != nil {
		return SourceView{}, sourceStoreError("update mqtt source setting", err)
	}
	if err := s.reconcile(ctx); err != nil {
		return SourceView{}, err
	}
	state, hasState, err := s.getStateForSource(ctx, updated.OrganizationID, updated.ID)
	if err != nil {
		return SourceView{}, err
	}
	return s.viewFor(updated, state, hasState), nil
}

func (s *SettingsService) SetEnabled(ctx context.Context, orgID, sourceID int64, enabled bool) (SourceView, error) {
	current, err := s.getConfig(ctx, orgID, sourceID)
	if err != nil {
		return SourceView{}, err
	}
	next := normalizeSourceConfig(current)
	next.Enabled = enabled
	if enabled {
		if err := s.validateSourceConfig(ctx, next); err != nil {
			return SourceView{}, err
		}
	} else {
		if err := s.quiesceSource(current.ID); err != nil {
			return SourceView{}, err
		}
	}
	updated, err := s.store.SetSourceConfigEnabled(ctx, orgID, sourceID, enabled)
	if err != nil {
		if !enabled {
			_ = s.reconcile(ctx)
		}
		return SourceView{}, sourceStoreError("set mqtt source enabled", err)
	}
	if err := s.reconcile(ctx); err != nil {
		return SourceView{}, err
	}
	state, hasState, err := s.getStateForSource(ctx, updated.OrganizationID, updated.ID)
	if err != nil {
		return SourceView{}, err
	}
	return s.viewFor(updated, state, hasState), nil
}

func (s *SettingsService) Delete(ctx context.Context, orgID, sourceID int64) error {
	if orgID <= 0 {
		return fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if sourceID <= 0 {
		return fleeterror.NewInvalidArgumentError("source_id must be set")
	}
	if err := s.store.DeleteDisabledSourceConfig(ctx, orgID, sourceID); err != nil {
		return sourceStoreError("delete mqtt source setting", err)
	}
	return s.reconcile(ctx)
}

func (s *SettingsService) getConfig(ctx context.Context, orgID, sourceID int64) (SourceConfig, error) {
	if orgID <= 0 {
		return SourceConfig{}, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if sourceID <= 0 {
		return SourceConfig{}, fleeterror.NewInvalidArgumentError("source_id must be set")
	}
	cfg, err := s.store.GetSourceConfigByOrg(ctx, orgID, sourceID)
	if err != nil {
		return SourceConfig{}, sourceStoreError("get mqtt source setting", err)
	}
	return cfg, nil
}

func (s *SettingsService) getStateForSource(ctx context.Context, orgID, sourceID int64) (SourceState, bool, error) {
	states, err := s.store.ListSourceStatesByOrg(ctx, orgID)
	if err != nil {
		return SourceState{}, false, fmt.Errorf("list mqtt source states: %w", err)
	}
	for _, state := range states {
		if state.SourceConfigID == sourceID {
			return state, true, nil
		}
	}
	return SourceState{}, false, nil
}

func (s *SettingsService) viewFor(cfg SourceConfig, state SourceState, hasState bool) SourceView {
	runtime := RuntimeStatus{State: RuntimeStateStopped}
	if !cfg.Enabled {
		runtime.State = RuntimeStateDisabled
	} else if s.runtime != nil {
		runtime = s.runtime.SourceRuntimeStatus(cfg.ID)
		if runtime.State == RuntimeStateUnspecified {
			runtime.State = RuntimeStateStopped
		}
	}
	stale := false
	if cfg.Enabled {
		stale = !hasState || state.LastReceivedAt.IsZero() || !state.LastReceivedAt.Add(cfg.StalenessThreshold).After(s.clock())
	}
	return SourceView{
		Config:   cfg,
		State:    state,
		HasState: hasState,
		Runtime:  runtime,
		Stale:    stale,
	}
}

func (s *SettingsService) validateSourceConfig(ctx context.Context, source SourceConfig) error {
	if source.OrganizationID <= 0 {
		return fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if source.ServiceUserID <= 0 {
		return fleeterror.NewInvalidArgumentError("service_user_id must be set")
	}
	if err := validateBoundedString("source_name", source.SourceName); err != nil {
		return err
	}
	if err := validateBoundedString("topic", source.Topic); err != nil {
		return err
	}
	if err := validateBoundedString("broker_primary_host", source.BrokerPrimaryHost); err != nil {
		return err
	}
	if err := validateBoundedString("broker_secondary_host", source.BrokerSecondaryHost); err != nil {
		return err
	}
	if source.BrokerPort <= 0 || source.BrokerPort > 65535 {
		return fleeterror.NewInvalidArgumentError("broker_port must be between 1 and 65535")
	}
	primary, secondary, ok := ResolveBrokerRoles(source.BrokerPrimaryHost, source.BrokerSecondaryHost)
	if !ok {
		return fleeterror.NewInvalidArgumentError("broker hosts must be distinct")
	}
	if err := validateBrokerTransport(source, primary, secondary); err != nil {
		return fleeterror.NewInvalidArgumentError(err.Error())
	}
	if err := validateBoundedString("mqtt_username", source.MQTTUsername); err != nil {
		return err
	}
	if source.MQTTPasswordEncrypted == "" {
		return fleeterror.NewInvalidArgumentError("mqtt_password is required")
	}
	password, err := s.cipher.Decrypt(source.MQTTPasswordEncrypted)
	if err != nil {
		return fleeterror.NewInvalidArgumentErrorf("mqtt_password cannot be decrypted: %v", err)
	}
	clear(password)
	if _, err := decoderForFormat(source.PayloadFormat); err != nil {
		return fleeterror.NewInvalidArgumentError(err.Error())
	}
	if source.StalenessThreshold <= 0 {
		return fleeterror.NewInvalidArgumentError("staleness_threshold_sec must be greater than zero")
	}
	return nil
}

func (s *SettingsService) encryptPassword(plaintext string) (string, error) {
	password := []byte(plaintext)
	defer clear(password)
	encrypted, err := s.cipher.Encrypt(password)
	if err != nil {
		return "", fmt.Errorf("encrypt mqtt password: %w", err)
	}
	return encrypted, nil
}

func (s *SettingsService) reconcile(context.Context) error {
	if s.runtime == nil {
		return nil
	}
	reconcileCtx, cancel := context.WithTimeout(context.Background(), s.reconcileTimeout)
	defer cancel()
	if err := s.runtime.Reconcile(reconcileCtx); err != nil {
		return fleeterror.NewUnavailableErrorf("mqtt source saved but runtime reload failed: %v", err)
	}
	return nil
}

func (s *SettingsService) quiesceSource(sourceID int64) error {
	if s.runtime == nil {
		return nil
	}
	reconcileCtx, cancel := context.WithTimeout(context.Background(), s.reconcileTimeout)
	defer cancel()
	if err := s.runtime.QuiesceSource(reconcileCtx, sourceID); err != nil {
		return fleeterror.NewUnavailableErrorf("mqtt source saved but runtime reload failed: %v", err)
	}
	return nil
}

func normalizeSourceConfig(source SourceConfig) SourceConfig {
	source.SourceName = strings.TrimSpace(source.SourceName)
	source.Topic = strings.TrimSpace(source.Topic)
	source.BrokerPrimaryHost = strings.TrimSpace(source.BrokerPrimaryHost)
	source.BrokerSecondaryHost = strings.TrimSpace(source.BrokerSecondaryHost)
	source.BrokerTransport = strings.ToLower(strings.TrimSpace(source.BrokerTransport))
	source.MQTTUsername = strings.TrimSpace(source.MQTTUsername)
	source.PayloadFormat = strings.TrimSpace(source.PayloadFormat)
	if source.BrokerPort == 0 {
		source.BrokerPort = defaultBrokerPort
	}
	if source.BrokerTransport == "" {
		source.BrokerTransport = brokerTransportTCP
	}
	if source.PayloadFormat == "" {
		source.PayloadFormat = payloadFormatTargetTimestamp
	}
	if source.StalenessThreshold <= 0 {
		source.StalenessThreshold = time.Duration(defaultStalenessThresholdSec) * time.Second
	}
	return source
}

func validateBoundedString(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fleeterror.NewInvalidArgumentErrorf("%s is required", field)
	}
	if len(value) > maxMQTTSourceStringLength {
		return fleeterror.NewInvalidArgumentErrorf("%s must be at most %d characters", field, maxMQTTSourceStringLength)
	}
	return nil
}

func mqttCredentialBindingChanged(current, next SourceConfig) bool {
	return current.BrokerPrimaryHost != next.BrokerPrimaryHost ||
		current.BrokerSecondaryHost != next.BrokerSecondaryHost ||
		current.BrokerPort != next.BrokerPort ||
		current.BrokerTransport != next.BrokerTransport ||
		current.MQTTUsername != next.MQTTUsername
}

func sourceStoreError(prefix string, err error) error {
	switch {
	case errors.Is(err, ErrSourceConfigNotFound):
		return fleeterror.NewNotFoundError("mqtt source not found")
	case errors.Is(err, ErrSourceConfigNameExists):
		return fleeterror.NewAlreadyExistsError("an MQTT curtailment source with this name already exists")
	case errors.Is(err, ErrSourceConfigDeleteBlocked):
		return fleeterror.NewFailedPreconditionError("disable the MQTT source before deleting it")
	default:
		return fmt.Errorf("%s: %w", prefix, err)
	}
}

func applyString(value *string, target *string) {
	if value != nil {
		*target = *value
	}
}

func applyInt32(value *int32, target *int32) {
	if value != nil {
		*target = *value
	}
}
