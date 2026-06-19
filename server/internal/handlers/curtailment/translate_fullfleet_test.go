package curtailment

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

func TestToRequestMode_FullFleetTakesNoParams(t *testing.T) {
	t.Parallel()

	mode, fk, err := toRequestMode(pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET, nil, false)
	require.NoError(t, err)
	assert.Equal(t, models.ModeFullFleet, mode)
	assert.Nil(t, fk, "full_fleet takes no fixed_kw params")
}

// FULL_FLEET must reject any set mode_params (fixed_kw, or reserved
// fixed_count / site_power_cap) rather than silently dropping them.
func TestToRequestMode_FullFleetRejectsParams(t *testing.T) {
	t.Parallel()

	_, _, err := toRequestMode(pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET, nil, true)
	require.Error(t, err, "FULL_FLEET with any mode params is a client bug, not a silent drop")
}

func TestToRequestMode_FixedKwRequiresParams(t *testing.T) {
	t.Parallel()

	_, _, err := toRequestMode(pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW, nil, false)
	require.Error(t, err, "FIXED_KW requires fixed_kw params")

	params := &pb.FixedKwParams{TargetKw: 100}
	mode, fk, err := toRequestMode(pb.CurtailmentMode_CURTAILMENT_MODE_UNSPECIFIED, params, true)
	require.NoError(t, err, "the unspecified default is FIXED_KW")
	assert.Equal(t, models.ModeFixedKw, mode)
	assert.Equal(t, params, fk)
}

func TestToRequestMode_ReservedModeRejected(t *testing.T) {
	t.Parallel()

	_, _, err := toRequestMode(pb.CurtailmentMode_CURTAILMENT_MODE_SITE_POWER_CAP, nil, false)
	require.Error(t, err)
}

func TestModeProto_FullFleet(t *testing.T) {
	t.Parallel()
	assert.Equal(t, pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET, modeProto(models.ModeFullFleet))
}

func TestStrategyNameMapsExplicitLeastEfficientFirst(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		models.StrategyLeastEfficientFirst,
		strategyName(pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST),
	)
}

// A full_fleet event echoes the (empty) full_fleet mode params on the wire.
func TestPopulateEventModeParams_FullFleet(t *testing.T) {
	t.Parallel()

	out := &pb.CurtailmentEvent{}
	populateEventModeParams(out, &models.Event{Mode: models.ModeFullFleet})
	assert.NotNil(t, out.GetFullFleet(), "full_fleet event sets the full_fleet oneof")
	assert.Nil(t, out.GetFixedKw())
}

func TestToStartResponse_ClosedLoopFullFleetReturnsActiveTargetlessEvent(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 6, 4, 11, 0, 0, 0, time.UTC)
	plan := &curtailment.Plan{
		StartedAt: &startedAt,
		Selected: []curtailment.SelectedDevice{
			{DeviceIdentifier: "miner-a", PowerW: 3000},
		},
	}
	req := &pb.StartCurtailmentRequest{
		Mode:  pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET,
		Scope: &pb.StartCurtailmentRequest_WholeOrg{WholeOrg: &pb.ScopeWholeOrg{}},
	}

	event := toStartResponse(plan, req).GetEvent()
	require.NotNil(t, event)
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE, event.GetState())
	assert.Empty(t, event.GetTargets())
	assert.Equal(t, int32(0), event.GetTargetRollup().GetTotal())
	require.NotNil(t, event.GetStartedAt())
	assert.Equal(t, plan.StartedAt.Unix(), event.GetStartedAt().AsTime().Unix())
	assert.Nil(t, event.GetEndedAt())
}

// Device-list FULL_FLEET remains an open-loop snapshot. Empty snapshots complete
// on arrival and the synchronous Start response must carry the completion time.
func TestToStartResponse_DeviceListFullFleetEmptyCarriesEndedAt(t *testing.T) {
	t.Parallel()

	endedAt := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	plan := &curtailment.Plan{EndedAt: &endedAt} // no Selected -> empty full_fleet
	req := &pb.StartCurtailmentRequest{
		Mode:  pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET,
		Scope: &pb.StartCurtailmentRequest_DeviceIdentifiers{DeviceIdentifiers: &pb.ScopeDeviceList{DeviceIdentifiers: []string{"miner-a"}}},
	}

	event := toStartResponse(plan, req).GetEvent()
	require.NotNil(t, event)
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_COMPLETED, event.GetState())
	require.NotNil(t, event.GetEndedAt(), "empty full_fleet Start response must carry the completion time")
	assert.Equal(t, endedAt.Unix(), event.GetEndedAt().AsTime().Unix())
}
