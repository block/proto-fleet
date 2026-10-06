package chat

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	chatv1 "github.com/block/proto-fleet/server/generated/grpc/chat/v1"
	"github.com/block/proto-fleet/server/generated/grpc/chat/v1/chatv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	chatdomain "github.com/block/proto-fleet/server/internal/domain/chat"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	config        *chatdomain.ConfigService
	agent         *chatdomain.Agent
	discoverer    chatdomain.ModelDiscoverer
	tools         chatdomain.ToolRegistry
	confirmations *chatdomain.ConfirmationBroker
}

func NewHandler(
	config *chatdomain.ConfigService,
	agent *chatdomain.Agent,
	discoverer chatdomain.ModelDiscoverer,
	tools chatdomain.ToolRegistry,
	confirmations *chatdomain.ConfirmationBroker,
) *Handler {
	return &Handler{config: config, agent: agent, discoverer: discoverer, tools: tools, confirmations: confirmations}
}

var _ chatv1connect.ChatServiceHandler = (*Handler)(nil)

func (h *Handler) GetLLMConfig(ctx context.Context, _ *connect.Request[chatv1.GetLLMConfigRequest]) (*connect.Response[chatv1.GetLLMConfigResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermAPIKeyManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	config, err := h.config.Get(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&chatv1.GetLLMConfigResponse{Config: configToProto(config)}), nil
}

func (h *Handler) DiscoverModels(ctx context.Context, req *connect.Request[chatv1.DiscoverModelsRequest]) (*connect.Response[chatv1.DiscoverModelsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermAPIKeyManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	config, err := h.config.DiscoveryRuntime(ctx, info.OrganizationID, chatdomain.DiscoverConfig{
		Provider:        providerFromProto(req.Msg.GetProvider()),
		APIKey:          req.Msg.GetApiKey(),
		BaseURL:         req.Msg.GetBaseUrl(),
		UseStoredAPIKey: req.Msg.GetUseStoredApiKey(),
	})
	if err != nil {
		return nil, err
	}
	models, err := h.discoverer.DiscoverModels(ctx, config)
	if err != nil {
		return nil, err
	}
	response := &chatv1.DiscoverModelsResponse{Models: make([]*chatv1.AvailableModel, 0, len(models))}
	for _, model := range models {
		response.Models = append(response.Models, &chatv1.AvailableModel{Id: model.ID, DisplayName: model.DisplayName})
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) UpdateLLMConfig(ctx context.Context, req *connect.Request[chatv1.UpdateLLMConfigRequest]) (*connect.Response[chatv1.UpdateLLMConfigResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermAPIKeyManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	config, err := h.config.Update(ctx, info.OrganizationID, chatdomain.UpdateConfig{
		Harness:          harnessFromProto(req.Msg.GetHarness()),
		Provider:         providerFromProto(req.Msg.GetProvider()),
		APIKey:           req.Msg.GetApiKey(),
		BaseURL:          req.Msg.GetBaseUrl(),
		Model:            req.Msg.GetModel(),
		GooseBaseURL:     req.Msg.GetGooseBaseUrl(),
		GooseSecret:      req.Msg.GetGooseSecret(),
		ClearAPIKey:      req.Msg.GetClearApiKey(),
		ClearGooseSecret: req.Msg.GetClearGooseSecret(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&chatv1.UpdateLLMConfigResponse{Config: configToProto(config)}), nil
}

func (h *Handler) SendMessage(ctx context.Context, req *connect.Request[chatv1.SendMessageRequest], stream *connect.ServerStream[chatv1.SendMessageResponse]) error {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{})
	if err != nil {
		return err
	}
	config, err := h.config.Runtime(ctx, info.OrganizationID)
	if err != nil {
		return err
	}
	history := make([]chatdomain.Message, 0, len(req.Msg.GetHistory()))
	for _, turn := range req.Msg.GetHistory() {
		role := "user"
		if turn.GetRole() == chatv1.ChatRole_CHAT_ROLE_ASSISTANT {
			role = "assistant"
		}
		history = append(history, chatdomain.Message{Role: role, Content: turn.GetContent()})
	}
	messageID := uuid.NewString()
	return h.agent.Run(ctx, config, history, req.Msg.GetContent(), h.tools, func(event chatdomain.Event) error {
		return stream.Send(eventToProto(messageID, event))
	})
}

func (h *Handler) ResolveToolConfirmation(ctx context.Context, req *connect.Request[chatv1.ResolveToolConfirmationRequest]) (*connect.Response[chatv1.ResolveToolConfirmationResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	decision := chatdomain.ConfirmationCancelled
	if req.Msg.GetDecision() == chatv1.ToolConfirmationDecision_TOOL_CONFIRMATION_DECISION_APPROVE {
		decision = chatdomain.ConfirmationApproved
	}
	if err := h.confirmations.Resolve(ctx, req.Msg.GetConfirmationId(), decision); err != nil {
		return nil, err
	}
	return connect.NewResponse(&chatv1.ResolveToolConfirmationResponse{}), nil
}

func configToProto(config chatdomain.ConfigView) *chatv1.LLMConfig {
	return &chatv1.LLMConfig{
		Harness:        harnessToProto(config.Harness),
		Provider:       providerToProto(config.Provider),
		HasApiKey:      config.HasAPIKey,
		BaseUrl:        config.BaseURL,
		Model:          config.Model,
		GooseBaseUrl:   config.GooseBaseURL,
		HasGooseSecret: config.HasGooseSecret,
		Configured:     config.Configured,
	}
}

func harnessFromProto(harness chatv1.AgentHarness) chatdomain.Harness {
	if harness == chatv1.AgentHarness_AGENT_HARNESS_GOOSE {
		return chatdomain.HarnessGoose
	}
	return chatdomain.HarnessNative
}

func harnessToProto(harness chatdomain.Harness) chatv1.AgentHarness {
	if harness == chatdomain.HarnessGoose {
		return chatv1.AgentHarness_AGENT_HARNESS_GOOSE
	}
	return chatv1.AgentHarness_AGENT_HARNESS_NATIVE
}

func providerFromProto(provider chatv1.LLMProvider) chatdomain.Provider {
	switch provider {
	case chatv1.LLMProvider_LLM_PROVIDER_UNSPECIFIED:
		return chatdomain.ProviderUnspecified
	case chatv1.LLMProvider_LLM_PROVIDER_OPENAI:
		return chatdomain.ProviderOpenAI
	case chatv1.LLMProvider_LLM_PROVIDER_ANTHROPIC:
		return chatdomain.ProviderAnthropic
	case chatv1.LLMProvider_LLM_PROVIDER_OLLAMA:
		return chatdomain.ProviderOllama
	case chatv1.LLMProvider_LLM_PROVIDER_CUSTOM:
		return chatdomain.ProviderCustom
	default:
		return chatdomain.ProviderUnspecified
	}
}

func providerToProto(provider chatdomain.Provider) chatv1.LLMProvider {
	switch provider {
	case chatdomain.ProviderUnspecified:
		return chatv1.LLMProvider_LLM_PROVIDER_UNSPECIFIED
	case chatdomain.ProviderOpenAI:
		return chatv1.LLMProvider_LLM_PROVIDER_OPENAI
	case chatdomain.ProviderAnthropic:
		return chatv1.LLMProvider_LLM_PROVIDER_ANTHROPIC
	case chatdomain.ProviderOllama:
		return chatv1.LLMProvider_LLM_PROVIDER_OLLAMA
	case chatdomain.ProviderCustom:
		return chatv1.LLMProvider_LLM_PROVIDER_CUSTOM
	default:
		return chatv1.LLMProvider_LLM_PROVIDER_UNSPECIFIED
	}
}

func eventToProto(messageID string, event chatdomain.Event) *chatv1.SendMessageResponse {
	response := &chatv1.SendMessageResponse{MessageId: messageID}
	switch event.Kind {
	case chatdomain.EventTextDelta:
		response.Event = &chatv1.SendMessageResponse_TextDelta{TextDelta: &chatv1.TextDelta{Content: event.Content}}
	case chatdomain.EventToolCall:
		response.Event = &chatv1.SendMessageResponse_ToolCall{ToolCall: &chatv1.ToolCall{
			Id: event.ToolCallID, Name: event.ToolName, Summary: event.Summary,
		}}
	case chatdomain.EventToolResult:
		response.Event = &chatv1.SendMessageResponse_ToolResult{ToolResult: &chatv1.ToolResult{
			Id: event.ToolCallID, Name: event.ToolName, Success: event.Success, Summary: event.Summary, Cancelled: event.Cancelled,
		}}
	case chatdomain.EventConfirmationRequired:
		confirmation := event.Confirmation
		details := make([]*chatv1.ToolConfirmationDetail, 0, len(confirmation.Details))
		for _, detail := range confirmation.Details {
			details = append(details, &chatv1.ToolConfirmationDetail{Label: detail.Label, Value: detail.Value})
		}
		response.Event = &chatv1.SendMessageResponse_ConfirmationRequired{ConfirmationRequired: &chatv1.ToolConfirmationRequired{
			ConfirmationId: event.ConfirmationID,
			ToolCallId:     event.ToolCallID,
			ToolName:       event.ToolName,
			Title:          confirmation.Title,
			Description:    confirmation.Description,
			ConfirmLabel:   confirmation.ConfirmLabel,
			Details:        details,
		}}
	case chatdomain.EventDone:
		response.Event = &chatv1.SendMessageResponse_Done{Done: &chatv1.Done{FinishReason: event.Summary}}
	}
	return response
}
