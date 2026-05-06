package curtailment

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
)

// TestCurtailmentEventStateNumericPins guards the AdminTerminateEventRequest
// validator that landed in BE-1.x (#173): the proto file pins
// `target_state` via `(buf.validate.field).enum.in: [6, 7]` — raw numeric
// tags. If a future commit reorders CurtailmentEventState or inserts a new
// state between RESTORING (3) and COMPLETED (4), the numeric pin silently
// rejects valid CANCELLED requests or accepts the wrong state. This test
// fails CI before that mismatch can ship.
//
// CANCELLED and FAILED are the only states AdminTerminateEvent should produce
// (a non-terminal event whose restore did not actually run cannot be reported
// as COMPLETED). The validator's numeric set must match these tags.
func TestCurtailmentEventStateNumericPins(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		int32(6),
		int32(pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED),
		"CANCELLED tag must remain 6 — AdminTerminateEventRequest validator pins on this number",
	)
	assert.Equal(t,
		int32(7),
		int32(pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_FAILED),
		"FAILED tag must remain 7 — AdminTerminateEventRequest validator pins on this number",
	)
}
