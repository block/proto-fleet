package mqttingest

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestNullStringFromTarget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		target    Target
		wantValid bool
		wantValue string
	}{
		{"OFF", TargetOff, true, "OFF"},
		{"ON", TargetOn, true, "ON"},
		{"Unknown becomes NULL", TargetUnknown, false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := nullStringFromTarget(tc.target)
			assert.Equal(t, tc.wantValid, got.Valid)
			if tc.wantValid {
				assert.Equal(t, tc.wantValue, got.String)
			}
		})
	}
}

func TestTargetFromNullString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, TargetOff, targetFromNullString(sql.NullString{String: "OFF", Valid: true}))
	assert.Equal(t, TargetOn, targetFromNullString(sql.NullString{String: "ON", Valid: true}))
	assert.Equal(t, TargetUnknown, targetFromNullString(sql.NullString{Valid: false}))
	assert.Equal(t, TargetUnknown, targetFromNullString(sql.NullString{String: "garbage", Valid: true}))
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
