package chat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestHTTPModelClient(client *http.Client) *HTTPModelClient {
	return &HTTPModelClient{publicHTTPClient: client, ollamaHTTPClient: client}
}

func TestHTTPModelClientOpenAIRequestAndToolResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/v1/chat/completions", req.URL.Path)
		assert.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		assert.Equal(t, "test-model", body["model"])
		assert.Equal(t, 0.3, body["temperature"])
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"list_sites","arguments":"{}"}}]}}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	client := newTestHTTPModelClient(server.Client())
	completion, err := client.Complete(t.Context(), RuntimeConfig{
		Provider:    ProviderOpenAI,
		APIKey:      "test-key",
		BaseURL:     server.URL + "/v1",
		Model:       "test-model",
		Temperature: 0.3,
	}, []Message{{Role: "user", Content: "List sites"}}, []ToolDefinition{{
		Name: "list_sites", InputSchema: map[string]any{"type": "object"},
	}})

	require.NoError(t, err)
	require.Len(t, completion.ToolCalls, 1)
	assert.Equal(t, "call-1", completion.ToolCalls[0].ID)
	assert.Equal(t, "list_sites", completion.ToolCalls[0].Name)
}

func TestHTTPModelClientOmitsTemperatureForGPT5(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		assert.Equal(t, "gpt-5", body["model"])
		assert.NotContains(t, body, "temperature")
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Hello"}}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	client := newTestHTTPModelClient(server.Client())
	completion, err := client.Complete(t.Context(), RuntimeConfig{
		Provider:    ProviderOpenAI,
		BaseURL:     server.URL + "/v1",
		Model:       "gpt-5",
		Temperature: 0.2,
	}, []Message{{Role: "user", Content: "Hello"}}, nil)

	require.NoError(t, err)
	assert.Equal(t, "Hello", completion.Content)
}

func TestHTTPModelClientDoesNotEchoProviderErrorBodiesOrReflectedCredentials(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, err := w.Write([]byte(`{"error":{"message":"rejected credential secret-key"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	client := newTestHTTPModelClient(server.Client())
	_, err := client.Complete(t.Context(), RuntimeConfig{
		Provider: ProviderOpenAI,
		APIKey:   "secret-key",
		BaseURL:  server.URL,
		Model:    "test-model",
	}, []Message{{Role: "user", Content: "Hello"}}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 400")
	assert.NotContains(t, err.Error(), "rejected credential")
	assert.NotContains(t, err.Error(), "secret-key")
}

func TestHTTPModelClientDiscoversOpenAIModels(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Equal(t, "/v1/models", req.URL.Path)
		assert.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"data":[{"id":"gpt-5.6"},{"id":"gpt-5.6"},{"id":"gpt-5.5-pro"},{"id":"gpt-5.4-2026-03-05"},{"id":"gpt-5.4-pro"},{"id":"gpt-5.3-codex"},{"id":"gpt-image-2"},{"id":"text-embedding-3-small"},{"id":""}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	models, err := newTestHTTPModelClient(server.Client()).DiscoverModels(t.Context(), RuntimeConfig{
		Provider: ProviderOpenAI,
		APIKey:   "test-key",
		BaseURL:  server.URL + "/v1",
	})

	require.NoError(t, err)
	assert.Equal(t, []AvailableModel{
		{ID: "gpt-5.6", DisplayName: "gpt-5.6"},
		{ID: "gpt-5.4-2026-03-05", DisplayName: "gpt-5.4-2026-03-05"},
	}, models)
}

func TestSupportsOpenAIChatAgentFlow(t *testing.T) {
	testCases := []struct {
		name      string
		modelID   string
		supported bool
	}{
		{name: "current alias", modelID: "gpt-5.6", supported: true},
		{name: "current tier", modelID: "gpt-5.6-terra", supported: true},
		{name: "dated snapshot", modelID: "gpt-5.4-mini-2026-03-17", supported: true},
		{name: "older agent model", modelID: "gpt-4.1-mini", supported: true},
		{name: "responses-only pro", modelID: "gpt-5.5-pro", supported: false},
		{name: "responses-only pro snapshot", modelID: "gpt-5.4-pro-2026-03-05", supported: false},
		{name: "responses-only codex", modelID: "gpt-5.3-codex", supported: false},
		{name: "specialized audio", modelID: "gpt-4o-mini-transcribe", supported: false},
		{name: "image generation", modelID: "gpt-image-2", supported: false},
		{name: "embedding", modelID: "text-embedding-3-small", supported: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.supported, supportsOpenAIChatAgentFlow(testCase.modelID))
		})
	}
}

func TestHTTPModelClientDiscoversAnthropicModels(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Equal(t, "/v1/models", req.URL.Path)
		assert.Equal(t, "1000", req.URL.Query().Get("limit"))
		assert.Equal(t, "test-key", req.Header.Get("X-Api-Key"))
		assert.Equal(t, "2023-06-01", req.Header.Get("Anthropic-Version"))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"data":[{"id":"claude-test","display_name":"Claude Test"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	models, err := newTestHTTPModelClient(server.Client()).DiscoverModels(t.Context(), RuntimeConfig{
		Provider: ProviderAnthropic,
		APIKey:   "test-key",
		BaseURL:  server.URL,
	})

	require.NoError(t, err)
	assert.Equal(t, []AvailableModel{{ID: "claude-test", DisplayName: "Claude Test"}}, models)
}

func TestHTTPModelClientDiscoversInstalledOllamaModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Equal(t, "/api/tags", req.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"models":[{"name":"llama-test:latest","model":"llama-test:latest"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	models, err := NewHTTPModelClient(ProviderEgressConfig{}).DiscoverModels(t.Context(), RuntimeConfig{
		Provider: ProviderOllama,
		BaseURL:  server.URL,
	})

	require.NoError(t, err)
	assert.Equal(t, []AvailableModel{{ID: "llama-test:latest", DisplayName: "llama-test:latest"}}, models)
}

func TestHTTPModelClientReportsServerPolicyForPrivateOllamaDestination(t *testing.T) {
	_, err := NewHTTPModelClient(ProviderEgressConfig{}).DiscoverModels(t.Context(), RuntimeConfig{
		Provider: ProviderOllama,
		BaseURL:  "http://10.0.0.20:11434",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked by the server egress policy")
	assert.NotContains(t, err.Error(), "10.0.0.20")
}

func TestHTTPModelClientGeneratesUniqueOllamaToolCallIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"message":{"tool_calls":[{"function":{"name":"list_sites","arguments":{}}}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	client := NewHTTPModelClient(ProviderEgressConfig{})
	config := RuntimeConfig{Provider: ProviderOllama, BaseURL: server.URL, Model: "llama-test"}
	first, err := client.Complete(t.Context(), config, []Message{{Role: "user", Content: "List sites"}}, nil)
	require.NoError(t, err)
	second, err := client.Complete(t.Context(), config, []Message{{Role: "user", Content: "List sites again"}}, nil)
	require.NoError(t, err)

	require.Len(t, first.ToolCalls, 1)
	require.Len(t, second.ToolCalls, 1)
	assert.NotEmpty(t, first.ToolCalls[0].ID)
	assert.NotEqual(t, first.ToolCalls[0].ID, second.ToolCalls[0].ID)
}

func TestHTTPModelClientRefusesPlainHTTPForCredentialProviders(t *testing.T) {
	client := NewHTTPModelClient(ProviderEgressConfig{})

	_, err := client.Complete(t.Context(), RuntimeConfig{
		Provider: ProviderOpenAI,
		BaseURL:  "http://203.0.113.10/v1",
		Model:    "gpt-test",
	}, []Message{{Role: "user", Content: "Hello"}}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTPS is required")
}
