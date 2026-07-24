package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/google/uuid"
)

const maxProviderResponseBytes = 4 << 20

// OpenAI's model-list response identifies models but does not expose endpoint
// or function-calling capabilities. Keep discovery conservative: the native
// agent currently depends on both Chat Completions and function calling.
var openAIChatAgentModelFamilies = []string{
	"gpt-5.6-sol",
	"gpt-5.6-terra",
	"gpt-5.6-luna",
	"gpt-5.6",
	"gpt-5.5",
	"gpt-5.4-mini",
	"gpt-5.4-nano",
	"gpt-5.4",
	"gpt-5.2",
	"gpt-5.1",
	"gpt-5-mini",
	"gpt-5-nano",
	"gpt-5",
	"gpt-4.1-mini",
	"gpt-4.1",
	"gpt-4o-mini",
}

type HTTPModelClient struct {
	publicHTTPClient *http.Client
	ollamaHTTPClient *http.Client
}

type AvailableModel struct {
	ID          string
	DisplayName string
}

type ModelDiscoverer interface {
	DiscoverModels(ctx context.Context, config RuntimeConfig) ([]AvailableModel, error)
}

func NewHTTPModelClient(egressConfig ProviderEgressConfig) *HTTPModelClient {
	return &HTTPModelClient{
		publicHTTPClient: newProviderHTTPClient(providerEgressPolicyFor(ProviderOpenAI, egressConfig)),
		ollamaHTTPClient: newProviderHTTPClient(providerEgressPolicyFor(ProviderOllama, egressConfig)),
	}
}

func (c *HTTPModelClient) Complete(ctx context.Context, config RuntimeConfig, messages []Message, tools []ToolDefinition) (Completion, error) {
	switch config.Provider {
	case ProviderUnspecified:
		return Completion{}, fleeterror.NewInvalidArgumentError("select an LLM provider")
	case ProviderOpenAI, ProviderCustom:
		return c.completeOpenAI(ctx, config, messages, tools)
	case ProviderAnthropic:
		return c.completeAnthropic(ctx, config, messages, tools)
	case ProviderOllama:
		return c.completeOllama(ctx, config, messages, tools)
	default:
		return Completion{}, fleeterror.NewInvalidArgumentErrorf("unsupported LLM provider %q", config.Provider)
	}
}

func (c *HTTPModelClient) DiscoverModels(ctx context.Context, config RuntimeConfig) ([]AvailableModel, error) {
	switch config.Provider {
	case ProviderOpenAI:
		return c.discoverOpenAIModels(ctx, config)
	case ProviderAnthropic:
		return c.discoverAnthropicModels(ctx, config)
	case ProviderOllama:
		return c.discoverOllamaModels(ctx, config)
	case ProviderCustom:
		return nil, fleeterror.NewInvalidArgumentError("model discovery is not available for custom providers")
	case ProviderUnspecified:
		return nil, fleeterror.NewInvalidArgumentError("select an LLM provider")
	default:
		return nil, fleeterror.NewInvalidArgumentErrorf("unsupported LLM provider %q", config.Provider)
	}
}

func (c *HTTPModelClient) discoverOpenAIModels(ctx context.Context, config RuntimeConfig) ([]AvailableModel, error) {
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	endpoint := providerEndpoint(config.BaseURL, "/models")
	headers := map[string]string{"Authorization": "Bearer " + config.APIKey}
	if err := c.getJSON(ctx, config.Provider, endpoint, headers, &response); err != nil {
		return nil, err
	}
	models := make([]AvailableModel, 0, len(response.Data))
	for _, model := range response.Data {
		if !supportsOpenAIChatAgentFlow(model.ID) {
			continue
		}
		models = append(models, AvailableModel{ID: model.ID, DisplayName: model.ID})
	}
	return normalizeAvailableModels(models), nil
}

func supportsOpenAIChatAgentFlow(modelID string) bool {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	for _, family := range openAIChatAgentModelFamilies {
		if modelID == family || strings.HasPrefix(modelID, family+"-20") {
			return true
		}
	}
	return false
}

func (c *HTTPModelClient) discoverAnthropicModels(ctx context.Context, config RuntimeConfig) ([]AvailableModel, error) {
	var response struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	endpoint := providerEndpoint(config.BaseURL, "/v1/models") + "?limit=1000"
	headers := map[string]string{
		"x-api-key":         config.APIKey,
		"anthropic-version": "2023-06-01",
	}
	if err := c.getJSON(ctx, config.Provider, endpoint, headers, &response); err != nil {
		return nil, err
	}
	models := make([]AvailableModel, 0, len(response.Data))
	for _, model := range response.Data {
		models = append(models, AvailableModel{ID: model.ID, DisplayName: model.DisplayName})
	}
	return normalizeAvailableModels(models), nil
}

func (c *HTTPModelClient) discoverOllamaModels(ctx context.Context, config RuntimeConfig) ([]AvailableModel, error) {
	var response struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	endpoint := providerEndpoint(config.BaseURL, "/api/tags")
	if err := c.getJSON(ctx, config.Provider, endpoint, nil, &response); err != nil {
		return nil, err
	}
	models := make([]AvailableModel, 0, len(response.Models))
	for _, model := range response.Models {
		id := model.Model
		if id == "" {
			id = model.Name
		}
		models = append(models, AvailableModel{ID: id, DisplayName: model.Name})
	}
	return normalizeAvailableModels(models), nil
}

func normalizeAvailableModels(models []AvailableModel) []AvailableModel {
	seen := make(map[string]struct{}, len(models))
	normalized := make([]AvailableModel, 0, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		model.DisplayName = strings.TrimSpace(model.DisplayName)
		if model.ID == "" {
			continue
		}
		if _, ok := seen[model.ID]; ok {
			continue
		}
		seen[model.ID] = struct{}{}
		if model.DisplayName == "" {
			model.DisplayName = model.ID
		}
		normalized = append(normalized, model)
	}
	return normalized
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func toOpenAIMessages(messages []Message) []openAIMessage {
	out := make([]openAIMessage, 0, len(messages))
	for _, message := range messages {
		converted := openAIMessage{
			Role:       message.Role,
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
		}
		for _, call := range message.ToolCalls {
			converted.ToolCalls = append(converted.ToolCalls, openAIToolCall{
				ID:   call.ID,
				Type: "function",
				Function: openAIFunctionCall{
					Name:      call.Name,
					Arguments: string(call.Arguments),
				},
			})
		}
		out = append(out, converted)
	}
	return out
}

func openAITools(tools []ToolDefinition) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			},
		})
	}
	return out
}

func (c *HTTPModelClient) completeOpenAI(ctx context.Context, config RuntimeConfig, messages []Message, tools []ToolDefinition) (Completion, error) {
	payload := map[string]any{
		"model":       config.Model,
		"messages":    toOpenAIMessages(messages),
		"tools":       openAITools(tools),
		"tool_choice": "auto",
	}
	// GPT-5-family models reject non-default temperature values. Omitting the
	// field lets OpenAI apply the model's supported default while preserving the
	// configured temperature for older OpenAI models.
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(config.Model)), "gpt-5") {
		payload["temperature"] = config.Temperature
	}
	var response struct {
		Choices []struct {
			Message openAIMessage `json:"message"`
		} `json:"choices"`
	}
	headers := map[string]string{}
	if config.APIKey != "" {
		headers["Authorization"] = "Bearer " + config.APIKey
	}
	endpoint := providerEndpoint(config.BaseURL, "/chat/completions")
	if err := c.doJSON(ctx, config.Provider, endpoint, headers, payload, &response); err != nil {
		return Completion{}, err
	}
	if len(response.Choices) == 0 {
		return Completion{}, fleeterror.NewUnavailableErrorf("LLM provider returned no choices")
	}
	message := response.Choices[0].Message
	completion := Completion{Content: message.Content}
	for _, call := range message.ToolCalls {
		arguments := json.RawMessage(call.Function.Arguments)
		if !json.Valid(arguments) {
			arguments = json.RawMessage(`{}`)
		}
		id := call.ID
		if id == "" {
			id = newToolCallID()
		}
		completion.ToolCalls = append(completion.ToolCalls, ModelToolCall{ID: id, Name: call.Function.Name, Arguments: arguments})
	}
	return completion, nil
}

type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

func toAnthropicMessages(messages []Message) (string, []anthropicMessage) {
	var system string
	out := make([]anthropicMessage, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			system = message.Content
			continue
		}
		if message.Role == "tool" {
			out = append(out, anthropicMessage{Role: "user", Content: []anthropicBlock{{
				Type:      "tool_result",
				ToolUseID: message.ToolCallID,
				Content:   message.Content,
			}}})
			continue
		}
		converted := anthropicMessage{Role: message.Role}
		if message.Content != "" {
			converted.Content = append(converted.Content, anthropicBlock{Type: "text", Text: message.Content})
		}
		for _, call := range message.ToolCalls {
			converted.Content = append(converted.Content, anthropicBlock{Type: "tool_use", ID: call.ID, Name: call.Name, Input: call.Arguments})
		}
		out = append(out, converted)
	}
	return system, out
}

func (c *HTTPModelClient) completeAnthropic(ctx context.Context, config RuntimeConfig, messages []Message, tools []ToolDefinition) (Completion, error) {
	system, converted := toAnthropicMessages(messages)
	anthropicTools := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		anthropicTools = append(anthropicTools, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": tool.InputSchema,
		})
	}
	payload := map[string]any{
		"model":       config.Model,
		"system":      system,
		"messages":    converted,
		"temperature": config.Temperature,
		"max_tokens":  2048,
		"tools":       anthropicTools,
	}
	var response struct {
		Content []anthropicBlock `json:"content"`
	}
	headers := map[string]string{
		"x-api-key":         config.APIKey,
		"anthropic-version": "2023-06-01",
	}
	endpoint := providerEndpoint(config.BaseURL, "/v1/messages")
	if err := c.doJSON(ctx, config.Provider, endpoint, headers, payload, &response); err != nil {
		return Completion{}, err
	}
	completion := Completion{}
	for _, block := range response.Content {
		switch block.Type {
		case "text":
			completion.Content += block.Text
		case "tool_use":
			arguments := block.Input
			if !json.Valid(arguments) {
				arguments = json.RawMessage(`{}`)
			}
			id := block.ID
			if id == "" {
				id = newToolCallID()
			}
			completion.ToolCalls = append(completion.ToolCalls, ModelToolCall{ID: id, Name: block.Name, Arguments: arguments})
		}
	}
	return completion, nil
}

func (c *HTTPModelClient) completeOllama(ctx context.Context, config RuntimeConfig, messages []Message, tools []ToolDefinition) (Completion, error) {
	payload := map[string]any{
		"model":    config.Model,
		"messages": toOpenAIMessages(messages),
		"stream":   false,
		"tools":    openAITools(tools),
		"options":  map[string]any{"temperature": config.Temperature},
	}
	var response struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	}
	endpoint := providerEndpoint(config.BaseURL, "/api/chat")
	if err := c.doJSON(ctx, config.Provider, endpoint, nil, payload, &response); err != nil {
		return Completion{}, err
	}
	completion := Completion{Content: response.Message.Content}
	for _, call := range response.Message.ToolCalls {
		arguments := call.Function.Arguments
		if !json.Valid(arguments) {
			arguments = json.RawMessage(`{}`)
		}
		completion.ToolCalls = append(completion.ToolCalls, ModelToolCall{
			ID:        newToolCallID(),
			Name:      call.Function.Name,
			Arguments: arguments,
		})
	}
	return completion, nil
}

func newToolCallID() string {
	return "tool-" + uuid.NewString()
}

func providerEndpoint(baseURL, suffix string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, suffix) {
		return base
	}
	if suffix == "/chat/completions" && strings.HasSuffix(base, "/v1") {
		return base + suffix
	}
	return base + suffix
}

func (c *HTTPModelClient) doJSON(ctx context.Context, provider Provider, endpoint string, headers map[string]string, payload, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fleeterror.NewInternalErrorf("marshal LLM request: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fleeterror.NewInternalErrorf("create LLM request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.executeJSON(provider, req, headers, target)
}

func (c *HTTPModelClient) getJSON(ctx context.Context, provider Provider, endpoint string, headers map[string]string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fleeterror.NewInternalErrorf("create LLM request: %v", err)
	}
	return c.executeJSON(provider, req, headers, target)
}

func (c *HTTPModelClient) executeJSON(provider Provider, req *http.Request, headers map[string]string, target any) error {
	if provider != ProviderOllama && req.URL.Scheme != "https" {
		return fleeterror.NewUnavailableErrorf("LLM provider request refused: HTTPS is required")
	}
	for name, value := range headers {
		if value != "" {
			req.Header.Set(name, value)
		}
	}
	httpClient := c.publicHTTPClient
	if provider == ProviderOllama {
		httpClient = c.ollamaHTTPClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(err, errProviderDestinationDisallowed) {
			return fleeterror.NewUnavailableErrorf("LLM provider destination is blocked by the server egress policy")
		}
		return fleeterror.NewUnavailableErrorf("LLM provider request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Provider-controlled response bodies are deliberately left unread. A
		// provider or intermediary can reflect request headers, including API
		// keys, in its error payload; returning that text would expose the secret
		// to the browser and error-logging interceptors.
		return fleeterror.NewUnavailableErrorf("LLM provider returned HTTP %d", resp.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseBytes+1))
	if err != nil {
		return fleeterror.NewUnavailableErrorf("LLM provider response could not be read")
	}
	if len(responseBody) > maxProviderResponseBytes {
		return fleeterror.NewUnavailableErrorf("LLM provider response exceeded %d bytes", maxProviderResponseBytes)
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return fleeterror.NewUnavailableErrorf("LLM provider returned an invalid JSON response")
	}
	return nil
}
