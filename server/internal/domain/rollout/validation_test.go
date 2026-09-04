package rollout_test

import (
	"fmt"
	"strings"
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

func TestApplyReleaseChannelFirmwareRequestValidation(t *testing.T) {
	t.Parallel()

	oversizedAssignments := make([]*rolloutv1.FirmwareAssignment, 101)
	for index := range oversizedAssignments {
		oversizedAssignments[index] = &rolloutv1.FirmwareAssignment{
			Manufacturer:   "Bitmain",
			Model:          fmt.Sprintf("model-%d", index),
			FirmwareFileId: "firmware",
		}
	}

	tests := []struct {
		name    string
		request *rolloutv1.ApplyReleaseChannelFirmwareRequest
		wantErr bool
	}{
		{
			name: "duplicate manufacturer and model pair is rejected",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-b"},
				},
			},
			wantErr: true,
		},
		{
			name: "case variants of one manufacturer and model pair are rejected",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: "bitMAIN", Model: "s21", FirmwareFileId: "firmware-b"},
				},
			},
			wantErr: true,
		},
		{
			name: "surrounding whitespace variants of one manufacturer and model pair are rejected",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: " Bitmain ", Model: "\tS21\n", FirmwareFileId: "firmware-b"},
				},
			},
			wantErr: true,
		},
		{
			name: "distinct normalized manufacturer and model pairs are accepted",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: " Bitmain ", Model: " S21 ", FirmwareFileId: "firmware-a"},
					{Manufacturer: " bitMAIN ", Model: " S19 ", FirmwareFileId: "firmware-b"},
				},
			},
		},
		{
			name: "same model under different manufacturers is accepted",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: "MicroBT", Model: "S21", FirmwareFileId: "firmware-b"},
				},
			},
		},
		{
			name: "different models under one manufacturer are accepted",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: "Bitmain", Model: "S19", FirmwareFileId: "firmware-b"},
				},
			},
		},
		{
			name: "length-prefixed composite keys do not collide",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "A", Model: "BC", FirmwareFileId: "firmware-a"},
					{Manufacturer: "AB", Model: "C", FirmwareFileId: "firmware-b"},
				},
			},
		},
		{
			name: "assignment count is bounded",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId:   1,
				Assignments: oversizedAssignments,
			},
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

func TestReleaseChannelModelGroupReportedVersionsValidation(t *testing.T) {
	t.Parallel()

	boundedVersions := []string{
		"1.0.0",
		"1.1.0",
		"1.2.0",
		"1.3.0",
		"1.4.0",
		"1.5.0",
		"1.6.0",
		"1.7.0",
		"1.8.0",
		"1.9.0",
	}
	oversizedVersions := append(append([]string{}, boundedVersions...), "2.0.0")

	tests := []struct {
		name       string
		modelGroup *rolloutv1.ReleaseChannelModelGroup
		wantErr    bool
	}{
		{
			name: "bounded truncated list is valid",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				ReportedVersions:     boundedVersions,
				ReportedVersionCount: 12,
			},
		},
		{
			name: "oversized list is rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				ReportedVersions:     oversizedVersions,
				ReportedVersionCount: 11,
			},
			wantErr: true,
		},
		{
			name: "count below returned list length is rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				ReportedVersions:     []string{"1.0.0", "2.0.0"},
				ReportedVersionCount: 1,
			},
			wantErr: true,
		},
		{
			name: "duplicate versions are rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				ReportedVersions:     []string{"1.0.0", "1.0.0"},
				ReportedVersionCount: 2,
			},
			wantErr: true,
		},
		{
			name: "oversized version is rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				ReportedVersions:     []string{strings.Repeat("v", 256)},
				ReportedVersionCount: 1,
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := protovalidate.Validate(test.modelGroup)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestReleaseChannelSummaryModelGroupsValidation(t *testing.T) {
	t.Parallel()

	boundedGroups := make([]*rolloutv1.ReleaseChannelModelGroup, 100)
	for index := range boundedGroups {
		boundedGroups[index] = &rolloutv1.ReleaseChannelModelGroup{
			Manufacturer: "Bitmain",
			Model:        fmt.Sprintf("model-%03d", index),
		}
	}
	oversizedGroups := append(
		append([]*rolloutv1.ReleaseChannelModelGroup{}, boundedGroups...),
		&rolloutv1.ReleaseChannelModelGroup{Manufacturer: "MicroBT", Model: "M60"},
	)

	tests := []struct {
		name    string
		summary *rolloutv1.ReleaseChannelSummary
		wantErr bool
	}{
		{
			name: "truncated summary is valid",
			summary: &rolloutv1.ReleaseChannelSummary{
				ModelGroups:     boundedGroups,
				ModelGroupCount: 101,
			},
		},
		{
			name: "101 groups are rejected",
			summary: &rolloutv1.ReleaseChannelSummary{
				ModelGroups:     oversizedGroups,
				ModelGroupCount: 101,
			},
			wantErr: true,
		},
		{
			name: "count below returned list length is rejected",
			summary: &rolloutv1.ReleaseChannelSummary{
				ModelGroups:     boundedGroups[:2],
				ModelGroupCount: 1,
			},
			wantErr: true,
		},
		{
			name: "count equal to returned list length is valid",
			summary: &rolloutv1.ReleaseChannelSummary{
				ModelGroups:     boundedGroups[:2],
				ModelGroupCount: 2,
			},
		},
		{
			name: "oversized group identity is rejected",
			summary: &rolloutv1.ReleaseChannelSummary{
				ModelGroups: []*rolloutv1.ReleaseChannelModelGroup{{
					Manufacturer: strings.Repeat("m", 256),
					Model:        "S21",
				}},
				ModelGroupCount: 1,
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := protovalidate.Validate(test.summary)
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
