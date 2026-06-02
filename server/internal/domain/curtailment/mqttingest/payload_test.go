package mqttingest

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodePayload_Valid(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	nowUnix := now.Unix()

	cases := []struct {
		name        string
		body        string
		wantTarget  Target
		wantPubUnix int64
	}{
		{
			name:        "OFF",
			body:        `{"target": 0, "timestamp": ` + itoa(nowUnix) + `}`,
			wantTarget:  TargetOff,
			wantPubUnix: nowUnix,
		},
		{
			name:        "ON",
			body:        `{"target": 100, "timestamp": ` + itoa(nowUnix) + `}`,
			wantTarget:  TargetOn,
			wantPubUnix: nowUnix,
		},
		{
			name:        "extra fields are ignored",
			body:        `{"target": 0, "timestamp": ` + itoa(nowUnix) + `, "unrelated": "ignored"}`,
			wantTarget:  TargetOff,
			wantPubUnix: nowUnix,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p, err := DecodePayload([]byte(tc.body), now)

			require.NoError(t, err)
			assert.Equal(t, tc.wantTarget, p.Target)
			assert.Equal(t, time.Unix(tc.wantPubUnix, 0).UTC(), p.PublishedAt)
		})
	}
}

func TestDecodePayload_Malformed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	farFuture := now.Add(48 * time.Hour).Unix()
	farPast := now.Add(-48 * time.Hour).Unix()

	cases := []struct {
		name        string
		body        string
		wantMessage string
	}{
		{"not JSON", `not json`, "invalid JSON"},
		{"empty object", `{}`, "missing target"},
		{"missing target", `{"timestamp": ` + itoa(now.Unix()) + `}`, "missing target"},
		{"missing timestamp", `{"target": 0}`, "missing timestamp"},
		{"target=50 invalid", `{"target": 50, "timestamp": ` + itoa(now.Unix()) + `}`, "outside {0, 100}"},
		{"target negative", `{"target": -1, "timestamp": ` + itoa(now.Unix()) + `}`, "outside {0, 100}"},
		{"target string", `{"target": "0", "timestamp": ` + itoa(now.Unix()) + `}`, "invalid JSON"},
		{"timestamp zero", `{"target": 0, "timestamp": 0}`, "non-positive"},
		{"timestamp negative", `{"target": 0, "timestamp": -1}`, "non-positive"},
		{
			name:        "timestamp far in future",
			body:        `{"target": 0, "timestamp": ` + itoa(farFuture) + `}`,
			wantMessage: "sanity window",
		},
		{
			name:        "timestamp far in past",
			body:        `{"target": 0, "timestamp": ` + itoa(farPast) + `}`,
			wantMessage: "sanity window",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodePayload([]byte(tc.body), now)

			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrMalformedPayload), "want ErrMalformedPayload, got %v", err)
			assert.True(t, strings.Contains(err.Error(), tc.wantMessage), "error %q must mention %q", err.Error(), tc.wantMessage)
		})
	}
}

func TestTarget_String(t *testing.T) {
	t.Parallel()

	cases := []struct {
		target Target
		want   string
	}{
		{TargetOff, "OFF"},
		{TargetOn, "ON"},
		{TargetUnknown, "UNKNOWN"},
		{Target(50), "target(50)"},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.target.String())
	}
}

func TestTarget_Predicates(t *testing.T) {
	t.Parallel()

	assert.True(t, TargetOff.IsOff())
	assert.False(t, TargetOff.IsOn())
	assert.True(t, TargetOn.IsOn())
	assert.False(t, TargetOn.IsOff())
	assert.False(t, TargetUnknown.IsOff())
	assert.False(t, TargetUnknown.IsOn())
}

// itoa formats an int64 for the table-driven test bodies above.
func itoa(n int64) string { return strconv.FormatInt(n, 10) }
