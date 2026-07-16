package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sequenceModel struct {
	completions []Completion
	calls       int
	messages    [][]Message
}

func (m *sequenceModel) Complete(_ context.Context, _ RuntimeConfig, messages []Message, _ []ToolDefinition) (Completion, error) {
	m.messages = append(m.messages, append([]Message(nil), messages...))
	completion := m.completions[m.calls]
	m.calls++
	return completion, nil
}

type recordingTools struct {
	called bool
	calls  int
}

func (*recordingTools) Definitions() []ToolDefinition {
	return []ToolDefinition{{Name: "list_sites", InputSchema: map[string]any{"type": "object"}}}
}

func (t *recordingTools) Execute(context.Context, string, json.RawMessage) (ToolOutput, error) {
	t.called = true
	t.calls++
	return ToolOutput{Content: `{"sites":[]}`, Summary: "Read 0 sites"}, nil
}

func TestAgentRunsToolThenStreamsFinalAnswer(t *testing.T) {
	model := &sequenceModel{completions: []Completion{
		{ToolCalls: []ModelToolCall{{ID: "call-1", Name: "list_sites", Arguments: json.RawMessage(`{}`)}}},
		{Content: "There are no configured sites."},
	}}
	tools := &recordingTools{}
	agent := NewAgent(model)
	var events []Event

	err := agent.Run(t.Context(), RuntimeConfig{Harness: HarnessNative}, nil, "How many sites?", tools, func(event Event) error {
		events = append(events, event)
		return nil
	})

	require.NoError(t, err)
	assert.True(t, tools.called)
	require.NotEmpty(t, model.messages)
	require.NotEmpty(t, model.messages[0])
	assert.Contains(t, model.messages[0][0].Content, "Use a Markdown table for status breakdowns")
	assert.Contains(t, model.messages[0][0].Content, "without repeating every value")
	assert.Equal(t, EventToolCall, events[0].Kind)
	assert.Equal(t, EventToolResult, events[1].Kind)
	assert.Equal(t, EventTextDelta, events[2].Kind)
	assert.Equal(t, "There are no configured sites.", events[2].Content)
	assert.Equal(t, EventDone, events[3].Kind)
}

func TestAgentRejectsUnavailableGooseHarness(t *testing.T) {
	agent := NewAgent(&sequenceModel{})

	err := agent.Run(t.Context(), RuntimeConfig{Harness: HarnessGoose}, nil, "Hello", &recordingTools{}, func(Event) error { return nil })

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Goose ACP harness")
}

func TestAgentRejectsTooManyToolCallsInOneTurnBeforeExecution(t *testing.T) {
	model := &sequenceModel{completions: []Completion{{ToolCalls: []ModelToolCall{
		{ID: "call-1", Name: "list_sites", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "list_sites", Arguments: json.RawMessage(`{}`)},
		{ID: "call-3", Name: "list_sites", Arguments: json.RawMessage(`{}`)},
		{ID: "call-4", Name: "list_sites", Arguments: json.RawMessage(`{}`)},
		{ID: "call-5", Name: "list_sites", Arguments: json.RawMessage(`{}`)},
	}}}}
	tools := &recordingTools{}
	agent := NewAgent(model)

	err := agent.Run(t.Context(), RuntimeConfig{Harness: HarnessNative}, nil, "List sites", tools, func(Event) error { return nil })

	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit is 4")
	assert.Zero(t, tools.calls)
}

func TestAgentEnforcesTotalToolCallBudgetBeforeExecutingOverflow(t *testing.T) {
	model := &sequenceModel{completions: []Completion{
		{ToolCalls: []ModelToolCall{
			{ID: "call-1", Name: "list_sites", Arguments: json.RawMessage(`{"request":1}`)},
			{ID: "call-2", Name: "list_sites", Arguments: json.RawMessage(`{"request":2}`)},
			{ID: "call-3", Name: "list_sites", Arguments: json.RawMessage(`{"request":3}`)},
			{ID: "call-4", Name: "list_sites", Arguments: json.RawMessage(`{"request":4}`)},
		}},
		{ToolCalls: []ModelToolCall{
			{ID: "call-5", Name: "list_sites", Arguments: json.RawMessage(`{"request":5}`)},
			{ID: "call-6", Name: "list_sites", Arguments: json.RawMessage(`{"request":6}`)},
			{ID: "call-7", Name: "list_sites", Arguments: json.RawMessage(`{"request":7}`)},
			{ID: "call-8", Name: "list_sites", Arguments: json.RawMessage(`{"request":8}`)},
		}},
		{ToolCalls: []ModelToolCall{{ID: "call-9", Name: "list_sites", Arguments: json.RawMessage(`{"request":9}`)}}},
	}}
	tools := &recordingTools{}
	agent := NewAgent(model)

	err := agent.Run(t.Context(), RuntimeConfig{Harness: HarnessNative}, nil, "Keep checking", tools, func(Event) error { return nil })

	require.Error(t, err)
	assert.Contains(t, err.Error(), "8-call tool budget")
	assert.Equal(t, 8, tools.calls)
}

func TestAgentDeduplicatesEquivalentToolCalls(t *testing.T) {
	model := &sequenceModel{completions: []Completion{
		{ToolCalls: []ModelToolCall{
			{ID: "call-1", Name: "list_sites", Arguments: json.RawMessage(`{"b":2,"a":1}`)},
			{ID: "call-2", Name: "list_sites", Arguments: json.RawMessage(`{"a":1,"b":2}`)},
		}},
		{Content: "Done."},
	}}
	tools := &recordingTools{}
	agent := NewAgent(model)

	err := agent.Run(t.Context(), RuntimeConfig{Harness: HarnessNative}, nil, "List sites", tools, func(Event) error { return nil })

	require.NoError(t, err)
	assert.Equal(t, 1, tools.calls)
	require.Len(t, model.messages, 2)
	toolMessages := 0
	for _, message := range model.messages[1] {
		if message.Role == "tool" {
			toolMessages++
		}
	}
	assert.Equal(t, 2, toolMessages, "each provider tool-call id still receives a result")
}
