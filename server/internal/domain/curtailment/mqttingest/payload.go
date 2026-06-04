// Package mqttingest is the in-process MQTT subscriber that consumes
// external curtailment signals and drives the curtailment service.
//
// The package is organized into pure-logic primitives (payload decode,
// precedence dedup, edge detection) and an I/O wrapper (the subscriber
// daemon that owns broker connections and persists state). Primitives
// are unit-testable without a broker.
package mqttingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Target is the canonical curtailment state extracted from a message. It is
// representation-neutral: a PayloadDecoder maps each integration's wire form
// onto these values, and the rest of the package — and the persisted state —
// speaks only this, never a specific feed's wire numbers.
type Target int

const (
	// TargetUnknown is the zero value, used before any message has been
	// observed (cold start).
	TargetUnknown Target = iota
	// TargetOff means the source must curtail.
	TargetOff
	// TargetOn means the source can operate at full power.
	TargetOn
)

// String renders the target in operator-readable form for logs and
// metric labels.
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

// IsOff returns true when the target is the curtail state.
func (t Target) IsOff() bool { return t == TargetOff }

// IsOn returns true when the target is the operate-fully state.
func (t Target) IsOn() bool { return t == TargetOn }

// Payload is a decoded MQTT message body in canonical form.
type Payload struct {
	// Target is the canonical curtailment state.
	Target Target
	// PublishedAt is the publisher's timestamp, normalized to UTC.
	PublishedAt time.Time
}

// ErrMalformedPayload is returned when a message body does not match the
// wire contract. The wrapped error carries the specific shape mismatch.
var ErrMalformedPayload = errors.New("malformed MQTT payload")

// timestampSanityWindow rejects a publisher timestamp more than a day
// from the receiver's clock (misconfigured publisher).
const timestampSanityWindow = 24 * time.Hour

// PayloadDecoder maps a raw MQTT message body to the canonical (Target,
// PublishedAt). One implementation per integration's wire contract, selected
// per source by curtailment_mqtt_source_config.payload_format. Everything
// downstream consumes only the canonical Payload, so a new integration is a new
// decoder plus a config value — no change to edge detection, the driver, etc.
type PayloadDecoder interface {
	Decode(body []byte, now time.Time) (Payload, error)
}

// payloadFormatTargetTimestamp is the registry key for the target/timestamp
// wire contract (the default MaestroOS-style feed).
const payloadFormatTargetTimestamp = "target_timestamp"

// payloadDecoders maps a payload_format to its decoder. Registering a new
// integration is a new entry here plus the matching source-config value; no
// schema change and nothing downstream is touched.
var payloadDecoders = map[string]PayloadDecoder{
	payloadFormatTargetTimestamp: targetTimestampDecoder{},
}

// decoderForFormat resolves a source's payload_format to its decoder, erroring
// on an unregistered format so a misconfigured source fails loudly at startup.
func decoderForFormat(format string) (PayloadDecoder, error) {
	if format == "" {
		// Unset → the default format (matches the DB column default), so an
		// in-memory SourceConfig without an explicit format still resolves.
		format = payloadFormatTargetTimestamp
	}
	d, ok := payloadDecoders[format]
	if !ok {
		return nil, fmt.Errorf("mqttingest: unknown payload_format %q", format)
	}
	return d, nil
}

// targetTimestampDecoder decodes {"target": 0|100, "timestamp": <unix_seconds>}:
// target 0 -> OFF, 100 -> ON. The wire's 0/100 values live only here.
type targetTimestampDecoder struct{}

const (
	// Wire-level target values for this format, mapped to the canonical Target
	// in Decode so nothing downstream sees them.
	ttWireTargetOff = 0
	ttWireTargetOn  = 100
)

// Decode implements PayloadDecoder. `now` is the receiver's current time
// (time.Now in production, injected in tests) for the sanity-window check.
func (targetTimestampDecoder) Decode(body []byte, now time.Time) (Payload, error) {
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
