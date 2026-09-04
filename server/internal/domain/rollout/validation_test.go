package rollout_test

import (
	"testing"

	"buf.build/go/protovalidate"
	rolloutv1 "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestRolloutBehaviorValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		behavior *rolloutv1.RolloutBehavior
		wantErr  bool
	}{
		{
			name:     "unspecified defaults are valid",
			behavior: &rolloutv1.RolloutBehavior{},
		},
		{
			name: "batched requires a positive batch size",
			behavior: &rolloutv1.RolloutBehavior{
				Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_BATCHED,
			},
			wantErr: true,
		},
		{
			name: "batched accepts one miner",
			behavior: &rolloutv1.RolloutBehavior{
				Method:    rolloutv1.RolloutMethod_ROLLOUT_METHOD_BATCHED,
				BatchSize: 1,
			},
		},
		{
			name: "pilot requires a positive pilot size",
			behavior: &rolloutv1.RolloutBehavior{
				Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE,
			},
			wantErr: true,
		},
		{
			name: "pilot accepts one miner",
			behavior: &rolloutv1.RolloutBehavior{
				Method:    rolloutv1.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE,
				PilotSize: 1,
			},
		},
		{
			name: "unknown method is rejected",
			behavior: &rolloutv1.RolloutBehavior{
				Method: rolloutv1.RolloutMethod(99),
			},
			wantErr: true,
		},
		{
			name: "unknown order is rejected",
			behavior: &rolloutv1.RolloutBehavior{
				Order: rolloutv1.RolloutOrder(99),
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := protovalidate.Validate(test.behavior)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestFirmwareAssignmentValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		assignment *rolloutv1.FirmwareAssignment
		wantErr    bool
	}{
		{
			name: "manufacturer and model are valid",
			assignment: &rolloutv1.FirmwareAssignment{
				Manufacturer: "Bitmain",
				Model:        "S21",
			},
		},
		{
			name: "manufacturer is required",
			assignment: &rolloutv1.FirmwareAssignment{
				Model: "S21",
			},
			wantErr: true,
		},
		{
			name: "model is required",
			assignment: &rolloutv1.FirmwareAssignment{
				Manufacturer: "Bitmain",
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := protovalidate.Validate(test.assignment)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRolloutPaginationValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request proto.Message
		wantErr bool
	}{
		{
			name:    "channel list accepts default page size",
			request: &rolloutv1.ListReleaseChannelsRequest{},
		},
		{
			name:    "channel list accepts maximum page",
			request: &rolloutv1.ListReleaseChannelsRequest{PageSize: 1000},
		},
		{
			name:    "channel list rejects oversized page",
			request: &rolloutv1.ListReleaseChannelsRequest{PageSize: 1001},
			wantErr: true,
		},
		{
			name:    "rollout list accepts default page size",
			request: &rolloutv1.ListRolloutsRequest{},
		},
		{
			name:    "rollout list rejects oversized page",
			request: &rolloutv1.ListRolloutsRequest{PageSize: 1001},
			wantErr: true,
		},
		{
			name:    "rollout devices accept maximum page",
			request: &rolloutv1.ListRolloutDevicesRequest{RolloutId: 1, PageSize: 1000},
		},
		{
			name:    "rollout devices reject oversized page",
			request: &rolloutv1.ListRolloutDevicesRequest{RolloutId: 1, PageSize: 1001},
			wantErr: true,
		},
		{
			name:    "channel miners accept maximum page",
			request: &rolloutv1.ListReleaseChannelMinersRequest{ChannelId: 1, PageSize: 1000},
		},
		{
			name:    "channel miners reject oversized page",
			request: &rolloutv1.ListReleaseChannelMinersRequest{ChannelId: 1, PageSize: 1001},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := protovalidate.Validate(test.request)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRolloutDetailsArePagedSeparately(t *testing.T) {
	t.Parallel()

	rolloutFields := (&rolloutv1.Rollout{}).ProtoReflect().Descriptor().Fields()
	require.Nil(t, rolloutFields.ByName("devices"))
	require.NotNil(t, rolloutFields.ByName("device_count"))

	modelGroupFields := (&rolloutv1.ReleaseChannelModelGroup{}).ProtoReflect().Descriptor().Fields()
	require.Nil(t, modelGroupFields.ByName("miners"))
	require.NotNil(t, modelGroupFields.ByName("miner_count"))

	channelSummary := (&rolloutv1.ReleaseChannelSummary{}).ProtoReflect().Descriptor()
	require.Nil(t, channelSummary.Fields().ByName("scope"))

	methods := rolloutv1.File_rollout_v1_rollout_proto.
		Services().
		ByName(protoreflect.Name("RolloutService")).
		Methods()
	listReleaseChannels := methods.ByName("ListReleaseChannels")
	require.NotNil(t, listReleaseChannels)
	channelsField := listReleaseChannels.Output().Fields().ByName("channels")
	require.NotNil(t, channelsField)
	require.Equal(t, channelSummary.FullName(), channelsField.Message().FullName())
	require.NotNil(t, methods.ByName("ListReleaseChannelMiners"))
	require.NotNil(t, methods.ByName("ListRolloutDevices"))
}

func TestManufacturerModelIdentitySchema(t *testing.T) {
	t.Parallel()

	messages := []struct {
		name       string
		descriptor protoreflect.MessageDescriptor
	}{
		{"model group", (&rolloutv1.ReleaseChannelModelGroup{}).ProtoReflect().Descriptor()},
		{"channel miner", (&rolloutv1.ReleaseChannelMiner{}).ProtoReflect().Descriptor()},
		{"firmware assignment", (&rolloutv1.FirmwareAssignment{}).ProtoReflect().Descriptor()},
		{"scope model count", (&rolloutv1.ReleaseChannelScopeModelCount{}).ProtoReflect().Descriptor()},
		{"rollout", (&rolloutv1.Rollout{}).ProtoReflect().Descriptor()},
		{"channel miner filter", (&rolloutv1.ListReleaseChannelMinersRequest{}).ProtoReflect().Descriptor()},
	}

	for _, message := range messages {
		t.Run(message.name, func(t *testing.T) {
			t.Parallel()
			require.NotNil(t, message.descriptor.Fields().ByName("manufacturer"))
			require.NotNil(t, message.descriptor.Fields().ByName("model"))
		})
	}
}
