package curtailment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

func TestToRequestMode_FullFleetTakesNoParams(t *testing.T) {
	t.Parallel()

	mode, fk, err := toRequestMode(pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET, nil)
	require.NoError(t, err)
	assert.Equal(t, models.ModeFullFleet, mode)
	assert.Nil(t, fk, "full_fleet takes no fixed_kw params")
}

// FULL_FLEET must reject fixed_kw params rather than silently dropping them.
func TestToRequestMode_FullFleetRejectsParams(t *testing.T) {
	t.Parallel()

	_, _, err := toRequestMode(
		pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET,
		&pb.FixedKwParams{TargetKw: 100},
	)
	require.Error(t, err, "FULL_FLEET with fixed_kw params is a client bug, not a silent drop")
}

func TestToRequestMode_FixedKwRequiresParams(t *testing.T) {
	t.Parallel()

	_, _, err := toRequestMode(pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW, nil)
	require.Error(t, err, "FIXED_KW requires fixed_kw params")

	params := &pb.FixedKwParams{TargetKw: 100}
	mode, fk, err := toRequestMode(pb.CurtailmentMode_CURTAILMENT_MODE_UNSPECIFIED, params)
	require.NoError(t, err, "the unspecified default is FIXED_KW")
	assert.Equal(t, models.ModeFixedKw, mode)
	assert.Equal(t, params, fk)
}

func TestToRequestMode_ReservedModeRejected(t *testing.T) {
	t.Parallel()

	_, _, err := toRequestMode(pb.CurtailmentMode_CURTAILMENT_MODE_SITE_POWER_CAP, nil)
	require.Error(t, err)
}

func TestModeProto_FullFleet(t *testing.T) {
	t.Parallel()
	assert.Equal(t, pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET, modeProto(models.ModeFullFleet))
}

// A full_fleet event echoes the (empty) full_fleet mode params on the wire.
func TestPopulateEventModeParams_FullFleet(t *testing.T) {
	t.Parallel()

	out := &pb.CurtailmentEvent{}
	populateEventModeParams(out, &models.Event{Mode: models.ModeFullFleet})
	assert.NotNil(t, out.GetFullFleet(), "full_fleet event sets the full_fleet oneof")
	assert.Nil(t, out.GetFixedKw())
}
