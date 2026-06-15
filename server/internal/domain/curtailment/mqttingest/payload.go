// Package mqttingest consumes MQTT curtailment source signals and records their
// runtime state.
package mqttingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Target is the canonical state decoded from an integration-specific payload.
type Target int

const (
	// TargetUnknown is the cold-start value.
	TargetUnknown Target = iota
	// TargetOff means curtail.
	TargetOff
	// TargetOn means full power.
	TargetOn
)

// String renders the target for logs and metrics.
func (t Target) String() string {
	switch t {
	case TargetOff:
		return "OFF"
	case TargetOn:
		return "ON"
	case TargetUnknown:
		return "UNKNOWN"
	default:
		return fmt.Sprintf("target(%d)", int(t))
	}
}

func (t Target) IsOff() bool { return t == TargetOff }

func (t Target) IsOn() bool { return t == TargetOn }

// Payload is a decoded MQTT message body in canonical form.
type Payload struct {
	Target Target
	// PublishedAt is the publisher timestamp normalized to UTC.
	PublishedAt time.Time
}

// ErrMalformedPayload is returned when a message body does not match the
// wire contract. The wrapped error carries the specific shape mismatch.
var ErrMalformedPayload = errors.New("malformed MQTT payload")

// timestampSanityWindow rejects badly skewed publisher clocks.
const timestampSanityWindow = 24 * time.Hour
const maxPayloadBytes = 1024

// PayloadDecoder maps a raw MQTT message to canonical state.
type PayloadDecoder interface {
	Decode(body []byte, now time.Time) (Payload, error)
}

// payloadFormatTargetTimestamp decodes the MaestroOS target/timestamp payload.
const payloadFormatTargetTimestamp = "target_timestamp"

var payloadDecoders = map[string]PayloadDecoder{
	payloadFormatTargetTimestamp: targetTimestampDecoder{},
}

// decoderForFormat resolves a configured payload_format.
func decoderForFormat(format string) (PayloadDecoder, error) {
	if format == "" {
		format = payloadFormatTargetTimestamp
	}
	d, ok := payloadDecoders[format]
	if !ok {
		return nil, fmt.Errorf("mqttingest: unknown payload_format %q", format)
	}
	return d, nil
}

// targetTimestampDecoder decodes {"target": 0|100, "timestamp": unix_seconds}.
type targetTimestampDecoder struct{}

const (
	ttWireTargetOff = 0
	ttWireTargetOn  = 100
)

func (targetTimestampDecoder) Decode(body []byte, now time.Time) (Payload, error) {
	if len(body) > maxPayloadBytes {
		return Payload{}, fmt.Errorf("%w: payload exceeds %d bytes", ErrMalformedPayload, maxPayloadBytes)
	}
	var raw struct {
		Target    *int   `json:"target"`
		Timestamp *int64 `json:"timestamp"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Payload{}, fmt.Errorf("%w: invalid JSON: %v", ErrMalformedPayload, err)
	}
	if raw.Target == nil {
		return Payload{}, fmt.Errorf("%w: missing target", ErrMalformedPayload)
	}
	if raw.Timestamp == nil {
		return Payload{}, fmt.Errorf("%w: missing timestamp", ErrMalformedPayload)
	}

	var target Target
	switch *raw.Target {
	case ttWireTargetOff:
		target = TargetOff
	case ttWireTargetOn:
		target = TargetOn
	default:
		return Payload{}, fmt.Errorf("%w: target=%d outside {0, 100}", ErrMalformedPayload, *raw.Target)
	}

	if *raw.Timestamp <= 0 {
		return Payload{}, fmt.Errorf("%w: timestamp=%d non-positive", ErrMalformedPayload, *raw.Timestamp)
	}
	publishedAt := time.Unix(*raw.Timestamp, 0).UTC()
	if delta := publishedAt.Sub(now); delta > timestampSanityWindow || delta < -timestampSanityWindow {
		return Payload{}, fmt.Errorf("%w: timestamp=%d outside ±%s sanity window", ErrMalformedPayload, *raw.Timestamp, timestampSanityWindow)
	}

	return Payload{Target: target, PublishedAt: publishedAt}, nil
}
