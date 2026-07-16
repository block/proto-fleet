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
}

func (*recordingTools) Definitions() []ToolDefinition {
	return []ToolDefinition{{Name: "list_sites", InputSchema: map[string]any{"type": "object"}}}
}

func (t *recordingTools) Execute(context.Context, string, json.RawMessage) (ToolOutput, error) {
	t.called = true
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
