package sqlstores

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalMetadata_Nil(t *testing.T) {
	result, err := marshalMetadata(nil)
	require.NoError(t, err)
	assert.False(t, result.Valid)
}

func TestMarshalMetadata_Empty(t *testing.T) {
	result, err := marshalMetadata(map[string]any{})
	require.NoError(t, err)
	assert.False(t, result.Valid)
}

func TestMarshalMetadata_Populated(t *testing.T) {
	m := map[string]any{
		"schedule_id":  "sched_123",
		"device_count": 42,
		"nested":       map[string]any{"key": "value"},
	}
	result, err := marshalMetadata(m)
	require.NoError(t, err)
	require.True(t, result.Valid)

	var decoded map[string]any
	err = json.Unmarshal(result.RawMessage, &decoded)
	require.NoError(t, err)
	assert.Equal(t, "sched_123", decoded["schedule_id"])
	assert.Equal(t, float64(42), decoded["device_count"])
	nested, ok := decoded["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", nested["key"])
}
