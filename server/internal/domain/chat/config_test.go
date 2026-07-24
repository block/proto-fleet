package chat

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoryConfigStore struct {
	record ConfigRecord
	hasRow bool
}

func (s *memoryConfigStore) Get(context.Context, int64) (ConfigRecord, error) {
	if !s.hasRow {
		return ConfigRecord{}, sql.ErrNoRows
	}
	return s.record, nil
}

func (s *memoryConfigStore) Upsert(_ context.Context, record ConfigRecord) (ConfigRecord, error) {
	s.record = record
	s.hasRow = true
	return record, nil
}

type prefixCipher struct{}

func (prefixCipher) Encrypt(plaintext []byte) (string, error)  { return "enc:" + string(plaintext), nil }
func (prefixCipher) Decrypt(ciphertext string) ([]byte, error) { return []byte(ciphertext[4:]), nil }

func TestConfigServiceGetReturnsSafeDefaults(t *testing.T) {
	service := NewConfigService(&memoryConfigStore{}, prefixCipher{}, ProviderEgressConfig{})

	view, err := service.Get(t.Context(), 42)

	require.NoError(t, err)
	assert.Equal(t, HarnessNative, view.Harness)
	assert.Equal(t, ProviderUnspecified, view.Provider)
	assert.False(t, view.HasAPIKey)
	assert.False(t, view.Configured)
}

func TestConfigServiceEncryptsAndNeverReturnsProviderSecret(t *testing.T) {
	store := &memoryConfigStore{}
	service := NewConfigService(store, prefixCipher{}, ProviderEgressConfig{})

	view, err := service.Update(t.Context(), 42, UpdateConfig{
		Harness:  HarnessNative,
		Provider: ProviderOpenAI,
		APIKey:   "secret-key",
		Model:    "model-name",
	})

	require.NoError(t, err)
	assert.Equal(t, "enc:secret-key", store.record.APIKeyEncrypted)
	assert.True(t, view.HasAPIKey)
	assert.True(t, view.Configured)
	assert.Equal(t, "https://api.openai.com/v1", view.BaseURL)
	assert.Equal(t, DefaultTemperature, store.record.Temperature)

	runtime, err := service.Runtime(t.Context(), 42)
	require.NoError(t, err)
	assert.Equal(t, "secret-key", runtime.APIKey)
	assert.Equal(t, DefaultTemperature, runtime.Temperature)
}

func TestConfigServiceRequiresExplicitProviderSelection(t *testing.T) {
	service := NewConfigService(&memoryConfigStore{}, prefixCipher{}, ProviderEgressConfig{})

	_, err := service.Update(t.Context(), 42, UpdateConfig{
		Harness:  HarnessNative,
		Provider: ProviderUnspecified,
		Model:    "model-name",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "select an LLM provider")
}

func TestConfigServiceBlankSecretPreservesExistingCiphertext(t *testing.T) {
	store := &memoryConfigStore{hasRow: true, record: ConfigRecord{
		OrganizationID:  42,
		Harness:         HarnessNative,
		Provider:        ProviderOpenAI,
		APIKeyEncrypted: "enc:old-key",
		BaseURL:         "https://api.openai.com/v1",
		Model:           "old-model",
		Temperature:     0.3,
	}}
	service := NewConfigService(store, prefixCipher{}, ProviderEgressConfig{})

	_, err := service.Update(t.Context(), 42, UpdateConfig{
		Harness:  HarnessNative,
		Provider: ProviderOpenAI,
		Model:    "new-model",
	})

	require.NoError(t, err)
	assert.Equal(t, "enc:old-key", store.record.APIKeyEncrypted)
	assert.Equal(t, "new-model", store.record.Model)
}

func TestConfigServiceDoesNotPreserveSecretForChangedProvider(t *testing.T) {
	store := &memoryConfigStore{hasRow: true, record: ConfigRecord{
		OrganizationID:  42,
		Harness:         HarnessNative,
		Provider:        ProviderOpenAI,
		APIKeyEncrypted: "enc:openai-key",
		BaseURL:         "https://api.openai.com/v1",
		Model:           "gpt-model",
	}}
	service := NewConfigService(store, prefixCipher{}, ProviderEgressConfig{})

	view, err := service.Update(t.Context(), 42, UpdateConfig{
		Harness:  HarnessNative,
		Provider: ProviderOllama,
		Model:    "llama-model",
	})

	require.NoError(t, err)
	assert.Empty(t, store.record.APIKeyEncrypted)
	assert.False(t, view.HasAPIKey)
}

func TestConfigServiceDoesNotPreserveSecretForChangedBaseURL(t *testing.T) {
	store := &memoryConfigStore{hasRow: true, record: ConfigRecord{
		OrganizationID:  42,
		Harness:         HarnessNative,
		Provider:        ProviderOpenAI,
		APIKeyEncrypted: "enc:openai-key",
		BaseURL:         "https://api.openai.com/v1",
		Model:           "gpt-model",
	}}
	service := NewConfigService(store, prefixCipher{}, ProviderEgressConfig{})

	view, err := service.Update(t.Context(), 42, UpdateConfig{
		Harness:  HarnessNative,
		Provider: ProviderOpenAI,
		BaseURL:  "https://llm-proxy.example.com/v1",
		Model:    "gpt-model",
	})

	require.NoError(t, err)
	assert.Empty(t, store.record.APIKeyEncrypted)
	assert.False(t, view.HasAPIKey)
	assert.False(t, view.Configured)
}

func TestConfigServiceRejectsCredentialBearingEndpoint(t *testing.T) {
	service := NewConfigService(&memoryConfigStore{}, prefixCipher{}, ProviderEgressConfig{})

	_, err := service.Update(t.Context(), 42, UpdateConfig{
		Harness:  HarnessNative,
		Provider: ProviderCustom,
		BaseURL:  "https://user:password@example.com/v1",
		Model:    "model-name",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot contain credentials")
}

func TestConfigServiceRequiresHTTPSForCredentialBearingProviders(t *testing.T) {
	service := NewConfigService(&memoryConfigStore{}, prefixCipher{}, ProviderEgressConfig{})

	_, err := service.Update(t.Context(), 42, UpdateConfig{
		Harness:  HarnessNative,
		Provider: ProviderOpenAI,
		APIKey:   "secret-key",
		BaseURL:  "http://api.example.com/v1",
		Model:    "model-name",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must use HTTPS")
}

func TestConfigServiceRejectsOllamaMetadataEndpoint(t *testing.T) {
	service := NewConfigService(&memoryConfigStore{}, prefixCipher{}, ProviderEgressConfig{})

	_, err := service.Update(t.Context(), 42, UpdateConfig{
		Harness:  HarnessNative,
		Provider: ProviderOllama,
		BaseURL:  "http://169.254.169.254",
		Model:    "model-name",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "internal")
}

func TestConfigServiceRejectsPrivateOllamaEndpointByDefault(t *testing.T) {
	service := NewConfigService(&memoryConfigStore{}, prefixCipher{}, ProviderEgressConfig{})

	_, err := service.Update(t.Context(), 42, UpdateConfig{
		Harness:  HarnessNative,
		Provider: ProviderOllama,
		BaseURL:  "http://10.0.0.20:11434",
		Model:    "model-name",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "private")
}

func TestConfigServiceAllowsPrivateOllamaEndpointWithDeploymentOptIn(t *testing.T) {
	store := &memoryConfigStore{}
	service := NewConfigService(store, prefixCipher{}, ProviderEgressConfig{AllowPrivateOllama: true})

	view, err := service.Update(t.Context(), 42, UpdateConfig{
		Harness:  HarnessNative,
		Provider: ProviderOllama,
		BaseURL:  "http://10.0.0.20:11434",
		Model:    "model-name",
	})

	require.NoError(t, err)
	assert.Equal(t, "http://10.0.0.20:11434", view.BaseURL)
}

func TestConfigServiceDiscoveryUsesOneTimeKeyAndProviderDefault(t *testing.T) {
	service := NewConfigService(&memoryConfigStore{}, prefixCipher{}, ProviderEgressConfig{})

	runtime, err := service.DiscoveryRuntime(t.Context(), 42, DiscoverConfig{
		Provider: ProviderOpenAI,
		APIKey:   "one-time-key",
	})

	require.NoError(t, err)
	assert.Equal(t, "one-time-key", runtime.APIKey)
	assert.Equal(t, "https://api.openai.com/v1", runtime.BaseURL)
}

func TestConfigServiceDiscoveryCanUseStoredKeyForSameProvider(t *testing.T) {
	store := &memoryConfigStore{hasRow: true, record: ConfigRecord{
		OrganizationID:  42,
		Provider:        ProviderAnthropic,
		APIKeyEncrypted: "enc:stored-key",
		BaseURL:         "https://api.anthropic.com",
	}}
	service := NewConfigService(store, prefixCipher{}, ProviderEgressConfig{})

	runtime, err := service.DiscoveryRuntime(t.Context(), 42, DiscoverConfig{
		Provider:        ProviderAnthropic,
		UseStoredAPIKey: true,
	})

	require.NoError(t, err)
	assert.Equal(t, "stored-key", runtime.APIKey)
	assert.Equal(t, "https://api.anthropic.com", runtime.BaseURL)
}

func TestConfigServiceDiscoveryRejectsStoredKeyForChangedBaseURL(t *testing.T) {
	store := &memoryConfigStore{hasRow: true, record: ConfigRecord{
		OrganizationID:  42,
		Provider:        ProviderAnthropic,
		APIKeyEncrypted: "enc:stored-key",
		BaseURL:         "https://api.anthropic.com",
	}}
	service := NewConfigService(store, prefixCipher{}, ProviderEgressConfig{})

	_, err := service.DiscoveryRuntime(t.Context(), 42, DiscoverConfig{
		Provider:        ProviderAnthropic,
		BaseURL:         "https://llm-proxy.example.com",
		UseStoredAPIKey: true,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "saved provider base URL")
}

func TestConfigServiceDiscoveryRejectsStoredKeyFromAnotherProvider(t *testing.T) {
	store := &memoryConfigStore{hasRow: true, record: ConfigRecord{
		OrganizationID:  42,
		Provider:        ProviderOpenAI,
		APIKeyEncrypted: "enc:stored-key",
	}}
	service := NewConfigService(store, prefixCipher{}, ProviderEgressConfig{})

	_, err := service.DiscoveryRuntime(t.Context(), 42, DiscoverConfig{
		Provider:        ProviderAnthropic,
		UseStoredAPIKey: true,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no stored API key")
}

func TestConfigServiceDiscoveryAllowsOllamaWithoutKey(t *testing.T) {
	service := NewConfigService(&memoryConfigStore{}, prefixCipher{}, ProviderEgressConfig{})

	runtime, err := service.DiscoveryRuntime(t.Context(), 42, DiscoverConfig{Provider: ProviderOllama})

	require.NoError(t, err)
	assert.Empty(t, runtime.APIKey)
	assert.Equal(t, "http://127.0.0.1:11434", runtime.BaseURL)
}

func TestConfigServiceDiscoveryRejectsCustomProvider(t *testing.T) {
	service := NewConfigService(&memoryConfigStore{}, prefixCipher{}, ProviderEgressConfig{})

	_, err := service.DiscoveryRuntime(t.Context(), 42, DiscoverConfig{
		Provider: ProviderCustom,
		APIKey:   "one-time-key",
		BaseURL:  "https://example.com/v1",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available for custom providers")
}
