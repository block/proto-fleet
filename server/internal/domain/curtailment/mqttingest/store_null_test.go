package mqttingest

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestNullInt16FromTarget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		target    Target
		wantValid bool
		wantValue int16
	}{
		{"OFF", TargetOff, true, 0},
		{"ON", TargetOn, true, 100},
		{"Unknown becomes NULL", TargetUnknown, false, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := nullInt16FromTarget(tc.target)
			assert.Equal(t, tc.wantValid, got.Valid)
			if tc.wantValid {
				assert.Equal(t, tc.wantValue, got.Int16)
			}
		})
	}
}

func TestTargetFromNullInt16(t *testing.T) {
	t.Parallel()

	assert.Equal(t, TargetOff, targetFromNullInt16(sql.NullInt16{Int16: 0, Valid: true}))
	assert.Equal(t, TargetOn, targetFromNullInt16(sql.NullInt16{Int16: 100, Valid: true}))
	assert.Equal(t, TargetUnknown, targetFromNullInt16(sql.NullInt16{Valid: false}))
}

func TestNullTimeFrom(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	got := nullTimeFrom(now)
	assert.True(t, got.Valid)
	assert.Equal(t, now, got.Time)

	got = nullTimeFrom(time.Time{})
	assert.False(t, got.Valid)
}

func TestTimeFromNullTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, now, timeFromNullTime(sql.NullTime{Time: now, Valid: true}))

	zero := timeFromNullTime(sql.NullTime{Valid: false})
	assert.True(t, zero.IsZero())
}

func TestNullStringFrom(t *testing.T) {
	t.Parallel()

	got := nullStringFrom("primary")
	assert.True(t, got.Valid)
	assert.Equal(t, "primary", got.String)

	got = nullStringFrom("")
	assert.False(t, got.Valid)
}

func TestNullUUIDFrom_AndBack(t *testing.T) {
	t.Parallel()

	id := uuid.New().String()

	got := nullUUIDFrom(id)
	assert.True(t, got.Valid)
	assert.Equal(t, id, got.UUID.String())

	// Round-trip back to string.
	assert.Equal(t, id, stringFromNullUUID(got))

	// Empty string round-trips to invalid.
	empty := nullUUIDFrom("")
	assert.False(t, empty.Valid)
	assert.Equal(t, "", stringFromNullUUID(empty))

	// Invalid string is treated as not-set (no panic).
	bad := nullUUIDFrom("not-a-uuid")
	assert.False(t, bad.Valid)
}
