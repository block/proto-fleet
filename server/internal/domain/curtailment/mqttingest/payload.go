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

// Target is the canonical curtailment state extracted from a message.
// The wire contract restricts target to 0 (OFF) or 100 (ON).
type Target int

const (
	// TargetUnknown is the zero value, used before any message has been
	// observed. Not a valid wire value.
	TargetUnknown Target = -1
	// TargetOff means the source must curtail.
	TargetOff Target = 0
	// TargetOn means the source can operate at full power.
	TargetOn Target = 100
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

// Payload is a decoded MQTT message body.
type Payload struct {
	// Target is the canonical state from the wire (0 or 100).
	Target Target
	// PublishedAt is the publisher's timestamp, normalized to UTC.
	PublishedAt time.Time
}

// ErrMalformedPayload is returned when a message body does not match the
// wire contract. The wrapped error carries the specific shape mismatch.
var ErrMalformedPayload = errors.New("malformed MQTT payload")

// timestampSanityWindow bounds the accepted publisher timestamp range
// against the receiver's clock. A timestamp more than a day in either
// direction is taken as evidence of a misconfigured publisher and the
// message is rejected.
const timestampSanityWindow = 24 * time.Hour

// DecodePayload parses a JSON message body and validates it against the
// wire contract. `now` is the receiver's current time; pass time.Now()
// in production, an injected clock in tests.
func DecodePayload(body []byte, now time.Time) (Payload, error) {
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
	case int(TargetOff):
		target = TargetOff
	case int(TargetOn):
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
