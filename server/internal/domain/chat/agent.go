package chat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	defaultMaxTurns     = 6
	maxDeltaRunes       = 160
	maxToolCallsPerTurn = 4
	maxToolCallsPerRun  = 8
)

const systemPrompt = `You are Proto Fleet AI, an operations assistant for bitcoin-miner fleets.
Use the available read-only tools whenever a question depends on live fleet state. Never invent fleet data.
Answer directly and concisely. Mention what you checked only when it clarifies the scope or an access limitation.
Format data for quick scanning:
- Use short prose for a single fact, an explanation, or a recommendation.
- Use a Markdown table for status breakdowns, comparisons, trends, or three or more related values or records.
- Choose columns that match the question. Do not force unrelated facts into one table.
- Put one entity or metric on each row, include units in headers when relevant, and right-align numeric columns with ---:.
- Distinguish zero from None or Unavailable. Never infer missing values.
- After a table, summarize the main conclusion in at most two sentences without repeating every value.
You cannot change device state in this release. If asked to perform a write, explain that write tools require a future confirmation flow.`

type Message struct {
	Role       string
	Content    string
	ToolCalls  []ModelToolCall
	ToolCallID string
}

type ModelToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type Completion struct {
	Content   string
	ToolCalls []ModelToolCall
}

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

type ToolOutput struct {
	Content string
	Summary string
}

type ToolRegistry interface {
	Definitions() []ToolDefinition
	Execute(ctx context.Context, name string, arguments json.RawMessage) (ToolOutput, error)
}

type ModelClient interface {
	Complete(ctx context.Context, config RuntimeConfig, messages []Message, tools []ToolDefinition) (Completion, error)
}

type EventKind string

const (
	EventTextDelta  EventKind = "text_delta"
	EventToolCall   EventKind = "tool_call"
	EventToolResult EventKind = "tool_result"
	EventDone       EventKind = "done"
)

type Event struct {
	Kind       EventKind
	Content    string
	ToolCallID string
	ToolName   string
	Summary    string
	Success    bool
}

type Agent struct {
	model    ModelClient
	maxTurns int
}

type cachedToolResult struct {
	output ToolOutput
	err    error
}

func NewAgent(model ModelClient) *Agent {
	return &Agent{model: model, maxTurns: defaultMaxTurns}
}

func (a *Agent) Run(
	ctx context.Context,
	config RuntimeConfig,
	history []Message,
	content string,
	tools ToolRegistry,
	emit func(Event) error,
) error {
	if config.Harness == HarnessGoose {
		return fleeterror.NewUnimplementedError("the Goose ACP harness is not available in this deployment; select the embedded harness")
	}

	messages := make([]Message, 0, len(history)+2)
	messages = append(messages, Message{Role: "system", Content: systemPrompt})
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: content})
	definitions := tools.Definitions()
	toolCallsUsed := 0
	toolResults := make(map[string]cachedToolResult)

	for range a.maxTurns {
		completion, err := a.model.Complete(ctx, config, messages, definitions)
		if err != nil {
			return err
		}
		if len(completion.ToolCalls) == 0 {
			if strings.TrimSpace(completion.Content) == "" {
				return fleeterror.NewUnavailableErrorf("the LLM provider returned an empty response")
			}
			for _, delta := range chunkText(completion.Content, maxDeltaRunes) {
				if err := emit(Event{Kind: EventTextDelta, Content: delta}); err != nil {
					return err
				}
			}
			return emit(Event{Kind: EventDone, Summary: "stop", Success: true})
		}
		if len(completion.ToolCalls) > maxToolCallsPerTurn {
			return fleeterror.NewFailedPreconditionErrorf(
				"agent requested %d tool calls in one turn; limit is %d",
				len(completion.ToolCalls),
				maxToolCallsPerTurn,
			)
		}
		if len(completion.ToolCalls) > maxToolCallsPerRun-toolCallsUsed {
			return fleeterror.NewFailedPreconditionErrorf("agent exceeded the %d-call tool budget", maxToolCallsPerRun)
		}
		toolCallsUsed += len(completion.ToolCalls)

		messages = append(messages, Message{
			Role:      "assistant",
			Content:   completion.Content,
			ToolCalls: completion.ToolCalls,
		})
		for _, call := range completion.ToolCalls {
			if err := emit(Event{
				Kind:       EventToolCall,
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Summary:    toolCallSummary(call.Name),
			}); err != nil {
				return err
			}

			cacheKey := toolCallCacheKey(call)
			cached, alreadyExecuted := toolResults[cacheKey]
			if !alreadyExecuted {
				cached.output, cached.err = tools.Execute(ctx, call.Name, call.Arguments)
				toolResults[cacheKey] = cached
			}
			output, toolErr := cached.output, cached.err
			resultContent := output.Content
			resultSummary := output.Summary
			if toolErr != nil {
				// Do not forward internal handler or authorization details to the
				// external model provider. The operator receives the safe activity
				// summary while the model only learns that the read was unavailable.
				resultContent = "Tool failed: fleet data is unavailable or access was denied"
				resultSummary = "Unable to read this fleet data"
			}
			if err := emit(Event{
				Kind:       EventToolResult,
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Summary:    resultSummary,
				Success:    toolErr == nil,
			}); err != nil {
				return err
			}
			messages = append(messages, Message{
				Role:       "tool",
				Content:    resultContent,
				ToolCallID: call.ID,
			})
		}
	}

	return fleeterror.NewFailedPreconditionErrorf("agent exceeded the %d-turn tool limit", a.maxTurns)
}

func toolCallCacheKey(call ModelToolCall) string {
	arguments := call.Arguments
	var normalized any
	if json.Unmarshal(arguments, &normalized) == nil {
		if encoded, err := json.Marshal(normalized); err == nil {
			arguments = encoded
		}
	}
	return call.Name + "\x00" + string(arguments)
}

func chunkText(content string, size int) []string {
	runes := []rune(content)
	chunks := make([]string, 0, (len(runes)+size-1)/size)
	for len(runes) > 0 {
		end := min(size, len(runes))
		chunks = append(chunks, string(runes[:end]))
		runes = runes[end:]
	}
	return chunks
}

func toolCallSummary(name string) string {
	switch name {
	case "get_miner_state_counts":
		return "Checking fleet health"
	case "list_sites":
		return "Reading site inventory"
	case "list_pools":
		return "Checking mining pools"
	default:
		return "Reading fleet data"
	}
}
