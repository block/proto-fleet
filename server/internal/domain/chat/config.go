package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type Harness string

const (
	HarnessNative Harness = "native"
	HarnessGoose  Harness = "goose"
)

type Provider string

const (
	ProviderUnspecified Provider = ""
	ProviderOpenAI      Provider = "openai"
	ProviderAnthropic   Provider = "anthropic"
	ProviderOllama      Provider = "ollama"
	ProviderCustom      Provider = "custom"
)

const DefaultTemperature = 0.2

type ConfigRecord struct {
	OrganizationID       int64
	Harness              Harness
	Provider             Provider
	APIKeyEncrypted      string
	BaseURL              string
	Model                string
	Temperature          float64
	GooseBaseURL         string
	GooseSecretEncrypted string
}

type ConfigView struct {
	Harness        Harness
	Provider       Provider
	HasAPIKey      bool
	BaseURL        string
	Model          string
	GooseBaseURL   string
	HasGooseSecret bool
	Configured     bool
}

type RuntimeConfig struct {
	Harness      Harness
	Provider     Provider
	APIKey       string
	BaseURL      string
	Model        string
	Temperature  float64
	GooseBaseURL string
	GooseSecret  string
}

type UpdateConfig struct {
	Harness          Harness
	Provider         Provider
	APIKey           string
	BaseURL          string
	Model            string
	GooseBaseURL     string
	GooseSecret      string
	ClearAPIKey      bool
	ClearGooseSecret bool
}

type DiscoverConfig struct {
	Provider        Provider
	APIKey          string
	BaseURL         string
	UseStoredAPIKey bool
}

type ConfigStore interface {
	Get(ctx context.Context, orgID int64) (ConfigRecord, error)
	Upsert(ctx context.Context, record ConfigRecord) (ConfigRecord, error)
}

type Cipher interface {
	Encrypt(plaintext []byte) (string, error)
	Decrypt(ciphertext string) ([]byte, error)
}

type ConfigService struct {
	store  ConfigStore
	cipher Cipher
}

func NewConfigService(store ConfigStore, cipher Cipher) *ConfigService {
	return &ConfigService{store: store, cipher: cipher}
}

func defaultConfig() ConfigView {
	return ConfigView{
		Harness:  HarnessNative,
		Provider: ProviderUnspecified,
	}
}

func (s *ConfigService) Get(ctx context.Context, orgID int64) (ConfigView, error) {
	if orgID == 0 {
		return ConfigView{}, fleeterror.NewUnauthenticatedError("organization id missing on session")
	}
	record, err := s.store.Get(ctx, orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultConfig(), nil
	}
	if err != nil {
		return ConfigView{}, err
	}
	return record.view(), nil
}

func (s *ConfigService) Update(ctx context.Context, orgID int64, input UpdateConfig) (ConfigView, error) {
	if orgID == 0 {
		return ConfigView{}, fleeterror.NewUnauthenticatedError("organization id missing on session")
	}
	if err := normalizeAndValidateConfig(&input); err != nil {
		return ConfigView{}, err
	}
	if input.ClearAPIKey && input.APIKey != "" {
		return ConfigView{}, fleeterror.NewInvalidArgumentError("api_key and clear_api_key cannot both be set")
	}
	if input.ClearGooseSecret && input.GooseSecret != "" {
		return ConfigView{}, fleeterror.NewInvalidArgumentError("goose_secret and clear_goose_secret cannot both be set")
	}

	existing, err := s.store.Get(ctx, orgID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ConfigView{}, err
	}
	record := ConfigRecord{
		OrganizationID:       orgID,
		Harness:              input.Harness,
		Provider:             input.Provider,
		BaseURL:              input.BaseURL,
		Model:                input.Model,
		Temperature:          DefaultTemperature,
		GooseBaseURL:         input.GooseBaseURL,
		GooseSecretEncrypted: existing.GooseSecretEncrypted,
	}
	// Provider credentials are destination-bound. Carrying a redacted secret
	// across a provider or endpoint change would let an update send the existing
	// credential to a destination that never supplied it.
	if existing.Provider == input.Provider && existing.BaseURL == input.BaseURL {
		record.APIKeyEncrypted = existing.APIKeyEncrypted
	}

	if input.ClearAPIKey {
		record.APIKeyEncrypted = ""
	} else if input.APIKey != "" {
		record.APIKeyEncrypted, err = s.cipher.Encrypt([]byte(input.APIKey))
		if err != nil {
			return ConfigView{}, fmt.Errorf("encrypt LLM API key: %w", err)
		}
	}
	if input.ClearGooseSecret {
		record.GooseSecretEncrypted = ""
	} else if input.GooseSecret != "" {
		record.GooseSecretEncrypted, err = s.cipher.Encrypt([]byte(input.GooseSecret))
		if err != nil {
			return ConfigView{}, fmt.Errorf("encrypt Goose secret: %w", err)
		}
	}

	stored, err := s.store.Upsert(ctx, record)
	if err != nil {
		return ConfigView{}, err
	}
	return stored.view(), nil
}

func (s *ConfigService) Runtime(ctx context.Context, orgID int64) (RuntimeConfig, error) {
	record, err := s.store.Get(ctx, orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return RuntimeConfig{}, fleeterror.NewFailedPreconditionError("configure an AI provider in Settings > Agents before starting a chat")
	}
	if err != nil {
		return RuntimeConfig{}, err
	}
	view := record.view()
	if !view.Configured {
		return RuntimeConfig{}, fleeterror.NewFailedPreconditionError("the selected AI provider configuration is incomplete")
	}

	runtime := RuntimeConfig{
		Harness:      record.Harness,
		Provider:     record.Provider,
		BaseURL:      record.BaseURL,
		Model:        record.Model,
		Temperature:  record.Temperature,
		GooseBaseURL: record.GooseBaseURL,
	}
	if record.APIKeyEncrypted != "" {
		plaintext, err := s.cipher.Decrypt(record.APIKeyEncrypted)
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf("decrypt LLM API key: %w", err)
		}
		runtime.APIKey = string(plaintext)
	}
	if record.GooseSecretEncrypted != "" {
		plaintext, err := s.cipher.Decrypt(record.GooseSecretEncrypted)
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf("decrypt Goose secret: %w", err)
		}
		runtime.GooseSecret = string(plaintext)
	}
	return runtime, nil
}

// DiscoveryRuntime resolves a one-time provider connection for model
// discovery. A newly entered API key is used only for this request; an
// operator can instead explicitly reuse the encrypted key already stored for
// the same provider.
func (s *ConfigService) DiscoveryRuntime(ctx context.Context, orgID int64, input DiscoverConfig) (RuntimeConfig, error) {
	if orgID == 0 {
		return RuntimeConfig{}, fleeterror.NewUnauthenticatedError("organization id missing on session")
	}
	if input.APIKey != "" && input.UseStoredAPIKey {
		return RuntimeConfig{}, fleeterror.NewInvalidArgumentError("api_key and use_stored_api_key cannot both be set")
	}
	if err := normalizeAndValidateDiscoveryConfig(&input); err != nil {
		return RuntimeConfig{}, err
	}

	runtime := RuntimeConfig{
		Provider: input.Provider,
		APIKey:   input.APIKey,
		BaseURL:  input.BaseURL,
	}
	if input.UseStoredAPIKey {
		record, err := s.store.Get(ctx, orgID)
		if errors.Is(err, sql.ErrNoRows) {
			return RuntimeConfig{}, fleeterror.NewFailedPreconditionError("no stored API key is available for the selected provider")
		}
		if err != nil {
			return RuntimeConfig{}, err
		}
		if record.Provider != input.Provider || record.APIKeyEncrypted == "" {
			return RuntimeConfig{}, fleeterror.NewFailedPreconditionError("no stored API key is available for the selected provider")
		}
		if record.BaseURL != input.BaseURL {
			return RuntimeConfig{}, fleeterror.NewFailedPreconditionError("the stored API key can only be used with its saved provider base URL; enter the key again to use a different endpoint")
		}
		plaintext, err := s.cipher.Decrypt(record.APIKeyEncrypted)
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf("decrypt LLM API key: %w", err)
		}
		runtime.APIKey = string(plaintext)
	}
	if input.Provider != ProviderOllama && runtime.APIKey == "" {
		return RuntimeConfig{}, fleeterror.NewInvalidArgumentError("enter an API key or use the stored key before fetching models")
	}
	return runtime, nil
}

func (record ConfigRecord) view() ConfigView {
	view := ConfigView{
		Harness:        record.Harness,
		Provider:       record.Provider,
		HasAPIKey:      record.APIKeyEncrypted != "",
		BaseURL:        record.BaseURL,
		Model:          record.Model,
		GooseBaseURL:   record.GooseBaseURL,
		HasGooseSecret: record.GooseSecretEncrypted != "",
	}
	if record.Harness == HarnessNative {
		view.Configured = record.Model != "" && (record.Provider == ProviderOllama || view.HasAPIKey)
	}
	// Goose is persisted now, but deliberately remains unavailable until the
	// remote ACP authentication/session adapter lands.
	return view
}

func normalizeAndValidateConfig(input *UpdateConfig) error {
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.GooseBaseURL = strings.TrimRight(strings.TrimSpace(input.GooseBaseURL), "/")
	input.Model = strings.TrimSpace(input.Model)

	switch input.Harness {
	case HarnessNative, HarnessGoose:
	default:
		return fleeterror.NewInvalidArgumentErrorf("unsupported agent harness %q", input.Harness)
	}
	switch input.Provider {
	case ProviderUnspecified:
		return fleeterror.NewInvalidArgumentError("select an LLM provider")
	case ProviderOpenAI:
		if input.BaseURL == "" {
			input.BaseURL = "https://api.openai.com/v1"
		}
	case ProviderAnthropic:
		if input.BaseURL == "" {
			input.BaseURL = "https://api.anthropic.com"
		}
	case ProviderOllama:
		if input.BaseURL == "" {
			input.BaseURL = "http://127.0.0.1:11434"
		}
	case ProviderCustom:
		if input.BaseURL == "" {
			return fleeterror.NewInvalidArgumentError("a base URL is required for a custom provider")
		}
	default:
		return fleeterror.NewInvalidArgumentErrorf("unsupported LLM provider %q", input.Provider)
	}
	if input.Model == "" {
		return fleeterror.NewInvalidArgumentError("an LLM model is required")
	}
	if err := validateProviderEndpointURL(input.Provider, input.BaseURL); err != nil {
		return err
	}
	if input.Harness == HarnessGoose {
		if input.GooseBaseURL == "" {
			return fleeterror.NewInvalidArgumentError("a Goose ACP base URL is required for the Goose harness")
		}
		if err := validateEndpointURL("Goose ACP base URL", input.GooseBaseURL); err != nil {
			return err
		}
	}
	return nil
}

func normalizeAndValidateDiscoveryConfig(input *DiscoverConfig) error {
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	switch input.Provider {
	case ProviderOpenAI:
		if input.BaseURL == "" {
			input.BaseURL = "https://api.openai.com/v1"
		}
	case ProviderAnthropic:
		if input.BaseURL == "" {
			input.BaseURL = "https://api.anthropic.com"
		}
	case ProviderOllama:
		if input.BaseURL == "" {
			input.BaseURL = "http://127.0.0.1:11434"
		}
	case ProviderCustom:
		return fleeterror.NewInvalidArgumentError("model discovery is not available for custom providers")
	case ProviderUnspecified:
		return fleeterror.NewInvalidArgumentError("select an LLM provider")
	default:
		return fleeterror.NewInvalidArgumentErrorf("unsupported LLM provider %q", input.Provider)
	}
	return validateProviderEndpointURL(input.Provider, input.BaseURL)
}

func validateEndpointURL(label, raw string) error {
	_, err := parseEndpointURL(label, raw)
	return err
}

func parseEndpointURL(label, raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fleeterror.NewInvalidArgumentErrorf("%s must be an absolute HTTP or HTTPS URL", label)
	}
	if parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return nil, fleeterror.NewInvalidArgumentErrorf("%s cannot contain credentials, a query, or a fragment", label)
	}
	return parsed, nil
}

func validateProviderEndpointURL(provider Provider, raw string) error {
	parsed, err := parseEndpointURL("provider base URL", raw)
	if err != nil {
		return err
	}
	if provider != ProviderOllama && parsed.Scheme != "https" {
		return fleeterror.NewInvalidArgumentError("provider base URL must use HTTPS unless the provider is Ollama")
	}

	host := parsed.Hostname()
	policy := providerEgressPolicyFor(provider)
	if provider != ProviderOllama {
		lowerHost := strings.ToLower(strings.TrimSuffix(host, "."))
		if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".localhost") {
			return fleeterror.NewInvalidArgumentError("provider base URL cannot target a private or internal address")
		}
	}
	if ip := net.ParseIP(host); ip != nil && !providerIPAllowed(policy, ip) {
		return fleeterror.NewInvalidArgumentError("provider base URL cannot target a private, internal, or reserved address")
	}
	return nil
}
