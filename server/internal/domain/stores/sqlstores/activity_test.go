package sqlstores

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/activity/models"
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

func TestIsCompletedBatchDuplicate(t *testing.T) {
	batchID := "b-123"
	uniqueErr := &pgconn.PgError{Code: pgErrCodeUniqueViolation, ConstraintName: completedBatchUniqueIndex}
	otherConstraintErr := &pgconn.PgError{Code: pgErrCodeUniqueViolation, ConstraintName: "activity_log_pkey"}
	otherPgErr := &pgconn.PgError{Code: "23503", ConstraintName: completedBatchUniqueIndex}

	cases := []struct {
		name  string
		event *models.Event
		err   error
		want  bool
	}{
		{
			name:  "nil event",
			event: nil,
			err:   uniqueErr,
			want:  false,
		},
		{
			name:  "nil error",
			event: &models.Event{Type: "reboot.completed", BatchID: &batchID},
			err:   nil,
			want:  false,
		},
		{
			name:  "missing batch id",
			event: &models.Event{Type: "reboot.completed"},
			err:   uniqueErr,
			want:  false,
		},
		{
			name:  "wrong event type suffix",
			event: &models.Event{Type: "reboot", BatchID: &batchID},
			err:   uniqueErr,
			want:  false,
		},
		{
			name:  "wrong pg code",
			event: &models.Event{Type: "reboot.completed", BatchID: &batchID},
			err:   otherPgErr,
			want:  false,
		},
		{
			name:  "wrong constraint name",
			event: &models.Event{Type: "reboot.completed", BatchID: &batchID},
			err:   otherConstraintErr,
			want:  false,
		},
		{
			name:  "non-pg error",
			event: &models.Event{Type: "reboot.completed", BatchID: &batchID},
			err:   errors.New("network blip"),
			want:  false,
		},
		{
			name:  "matching completion duplicate",
			event: &models.Event{Type: "reboot.completed", BatchID: &batchID},
			err:   uniqueErr,
			want:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isCompletedBatchDuplicate(tc.event, tc.err)
			assert.Equal(t, tc.want, got)
		})
	}
}
