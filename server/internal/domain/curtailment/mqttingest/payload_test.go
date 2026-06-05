package mqttingest

import (
	"encoding/json"
	"errors"
	"fmt"
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

			p, err := targetTimestampDecoder{}.Decode([]byte(tc.body), now)

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
		{
			name:        "oversized payload",
			body:        strings.Repeat(" ", maxPayloadBytes+1),
			wantMessage: "payload exceeds",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := targetTimestampDecoder{}.Decode([]byte(tc.body), now)

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

// stateStringDecoder proves a different wire schema can feed the same
// canonical Payload.
type stateStringDecoder struct{}

func (stateStringDecoder) Decode(body []byte, _ time.Time) (Payload, error) {
	var raw struct {
		State string `json:"state"`
		At    int64  `json:"at"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Payload{}, fmt.Errorf("%w: %v", ErrMalformedPayload, err)
	}
	switch raw.State {
	case "on":
		return Payload{Target: TargetOn, PublishedAt: time.Unix(raw.At, 0).UTC()}, nil
	case "off":
		return Payload{Target: TargetOff, PublishedAt: time.Unix(raw.At, 0).UTC()}, nil
	default:
		return Payload{}, fmt.Errorf("%w: state=%q", ErrMalformedPayload, raw.State)
	}
}

// An arbitrary JSON shape decodes to the canonical Payload consumed downstream.
func TestPayloadDecoder_AlternateShapeYieldsCanonical(t *testing.T) {
	t.Parallel()

	var dec PayloadDecoder = stateStringDecoder{}
	p, err := dec.Decode([]byte(`{"state":"off","at":1000}`), time.Unix(1000, 0))
	require.NoError(t, err)
	assert.Equal(t, TargetOff, p.Target)
	assert.Equal(t, time.Unix(1000, 0).UTC(), p.PublishedAt)
}

// The registry resolves a known format and rejects an unregistered one, so a
// misconfigured source fails loudly at startup.
func TestDecoderForFormat(t *testing.T) {
	t.Parallel()

	d, err := decoderForFormat(payloadFormatTargetTimestamp)
	require.NoError(t, err)
	assert.IsType(t, targetTimestampDecoder{}, d)

	// Unset format resolves to the default (the DB column's default).
	def, err := decoderForFormat("")
	require.NoError(t, err)
	assert.IsType(t, targetTimestampDecoder{}, def)

	_, err = decoderForFormat("nope")
	require.Error(t, err)
}
