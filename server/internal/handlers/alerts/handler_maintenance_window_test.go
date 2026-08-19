package alerts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	alertsv1 "github.com/block/proto-fleet/server/generated/grpc/alerts/v1"
	"github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestProtoToMaintenanceWindowMapping(t *testing.T) {
	starts := timestamppb.New(time.Unix(1000, 0))
	ends := timestamppb.New(time.Unix(2000, 0))

	dom, err := protoToMaintenanceWindow("5", &alertsv1.MaintenanceWindowScope{
		RuleIds:    []string{"rule-a"},
		ChannelIds: []string{"3"},
	}, starts, ends, "planned")
	require.NoError(t, err)
	assert.Equal(t, "5", dom.ID)
	assert.Equal(t, []string{"rule-a"}, dom.RuleIDs)
	assert.Equal(t, []string{"3"}, dom.ChannelIDs)
	assert.Equal(t, time.Unix(1000, 0).UTC(), dom.StartsAt.UTC())
	assert.Equal(t, time.Unix(2000, 0).UTC(), dom.EndsAt.UTC())

	_, err = protoToMaintenanceWindow("", nil, starts, ends, "")
	require.Error(t, err, "scope message is required")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = protoToMaintenanceWindow("", &alertsv1.MaintenanceWindowScope{}, nil, ends, "")
	require.Error(t, err, "starts_at is required")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestMaintenanceWindowToProtoOmitsZeroEndsAt(t *testing.T) {
	out := maintenanceWindowToProto(alerts.MaintenanceWindow{
		ID:       "5",
		RuleIDs:  []string{"rule-a"},
		StartsAt: time.Unix(1000, 0),
	})
	require.NotNil(t, out.GetScope())
	assert.Equal(t, []string{"rule-a"}, out.GetScope().GetRuleIds())
	assert.Empty(t, out.GetScope().GetChannelIds())
	assert.Nil(t, out.GetEndsAt())
}
