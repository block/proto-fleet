package fleetmanagement

import (
	"context"
	"math"
	"strings"
	"testing"

	"connectrpc.com/authn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	commonpb "github.com/proto-at-block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/proto-at-block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/deviceresolver"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/session"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces"
	storemocks "github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

func int32Ptr(v int32) *int32 { return &v }

func sectionPtr(v pb.CharacterSection) *pb.CharacterSection { return &v }

func baseProps() interfaces.DeviceRenameProperties {
	return interfaces.DeviceRenameProperties{
		DeviceIdentifier:   "dev-1",
		DiscoveredDeviceID: 1,
		MacAddress:         "AA:BB:CC:DD:EE:FF",
		SerialNumber:       "SN1234567",
		Model:              "S19Pro",
		Manufacturer:       "Bitmain",
		WorkerName:         "worker-01",
	}
}

func float64Ptr(v float64) *float64 { return &v }

func stringPtr(v string) *string { return &v }

// TestFormatCounter verifies zero-padding across different scales.
func TestFormatCounter(t *testing.T) {
	tests := []struct {
		value    int
		scale    int
		expected string
	}{
		{0, 1, "0"},
		{1, 1, "1"},
		{9, 1, "9"},
		{10, 1, "10"},
		{0, 3, "000"},
		{1, 3, "001"},
		{42, 3, "042"},
		{1000, 3, "1000"},
		{0, 6, "000000"},
		{999999, 6, "999999"},
	}

	for _, tc := range tests {
		result := formatCounter(tc.value, tc.scale)
		assert.Equal(t, tc.expected, result, "formatCounter(%d, %d)", tc.value, tc.scale)
	}
}

// TestGenerateName_Counter verifies that counters increment per device index.
func TestGenerateName_Counter(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_Counter{Counter: &pb.CounterProperty{CounterStart: 1, CounterScale: 3}}},
		},
		Separator: "-",
	}
	props := baseProps()

	name0, err := generateName(cfg, props, 0)
	require.NoError(t, err)
	assert.Equal(t, "001", name0)

	name2, err := generateName(cfg, props, 2)
	require.NoError(t, err)
	assert.Equal(t, "003", name2)
}

// TestGenerateName_StringAndCounter verifies prefix+counter+suffix combining.
func TestGenerateName_StringAndCounter(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_StringAndCounter{StringAndCounter: &pb.StringAndCounterProperty{
				Prefix:       "rig-",
				Suffix:       "-prod",
				CounterStart: 10,
				CounterScale: 2,
			}}},
		},
		Separator: "",
	}
	props := baseProps()

	name, err := generateName(cfg, props, 0)
	require.NoError(t, err)
	assert.Equal(t, "rig-10-prod", name)

	name, err = generateName(cfg, props, 3)
	require.NoError(t, err)
	assert.Equal(t, "rig-13-prod", name)
}

// TestGenerateName_StringOnly verifies a static string is returned as-is.
func TestGenerateName_StringOnly(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: "warehouse-A"}}},
		},
		Separator: "-",
	}

	name, err := generateName(cfg, baseProps(), 0)
	require.NoError(t, err)
	assert.Equal(t, "warehouse-A", name)
}

func TestSortDevicePropsForRename_NameAscending(t *testing.T) {
	deviceProps := []interfaces.DeviceRenameProperties{
		{
			DeviceIdentifier:   "dev-2",
			DiscoveredDeviceID: 2,
			CustomName:         "Zulu",
		},
		{
			DeviceIdentifier:   "dev-3",
			DiscoveredDeviceID: 3,
			Manufacturer:       "Bitmain",
			Model:              "S19",
		},
		{
			DeviceIdentifier:   "dev-1",
			DiscoveredDeviceID: 1,
			CustomName:         "Alpha",
		},
	}

	sortDevicePropsForRename(deviceProps, &interfaces.SortConfig{
		Field:     interfaces.SortFieldName,
		Direction: interfaces.SortDirectionAsc,
	})

	assert.Equal(t, []string{"dev-1", "dev-3", "dev-2"}, []string{
		deviceProps[0].DeviceIdentifier,
		deviceProps[1].DeviceIdentifier,
		deviceProps[2].DeviceIdentifier,
	})
}

func TestSortDevicePropsForRename_HashrateDescNullsLast(t *testing.T) {
	deviceProps := []interfaces.DeviceRenameProperties{
		{
			DeviceIdentifier:   "dev-1",
			DiscoveredDeviceID: 1,
			Hashrate:           float64Ptr(90),
		},
		{
			DeviceIdentifier:   "dev-2",
			DiscoveredDeviceID: 2,
		},
		{
			DeviceIdentifier:   "dev-3",
			DiscoveredDeviceID: 3,
			Hashrate:           float64Ptr(110),
		},
	}

	sortDevicePropsForRename(deviceProps, &interfaces.SortConfig{
		Field:     interfaces.SortFieldHashrate,
		Direction: interfaces.SortDirectionDesc,
	})

	assert.Equal(t, []string{"dev-3", "dev-1", "dev-2"}, []string{
		deviceProps[0].DeviceIdentifier,
		deviceProps[1].DeviceIdentifier,
		deviceProps[2].DeviceIdentifier,
	})
}

func TestSortDevicePropsForRename_HashrateAscNaNAfterFinite(t *testing.T) {
	deviceProps := []interfaces.DeviceRenameProperties{
		{
			DeviceIdentifier:   "dev-1",
			DiscoveredDeviceID: 1,
			Hashrate:           float64Ptr(math.NaN()),
		},
		{
			DeviceIdentifier:   "dev-2",
			DiscoveredDeviceID: 2,
			Hashrate:           float64Ptr(90),
		},
		{
			DeviceIdentifier:   "dev-3",
			DiscoveredDeviceID: 3,
		},
	}

	sortDevicePropsForRename(deviceProps, &interfaces.SortConfig{
		Field:     interfaces.SortFieldHashrate,
		Direction: interfaces.SortDirectionAsc,
	})

	assert.Equal(t, []string{"dev-2", "dev-1", "dev-3"}, []string{
		deviceProps[0].DeviceIdentifier,
		deviceProps[1].DeviceIdentifier,
		deviceProps[2].DeviceIdentifier,
	})
}

func TestSortDevicePropsForRename_ModelAscNullsLast(t *testing.T) {
	deviceProps := []interfaces.DeviceRenameProperties{
		{
			DeviceIdentifier:   "dev-2",
			DiscoveredDeviceID: 2,
			Model:              "",
			ModelSortValue:     stringPtr(""),
		},
		{
			DeviceIdentifier:   "dev-3",
			DiscoveredDeviceID: 3,
			Model:              "S21",
			ModelSortValue:     stringPtr("S21"),
		},
		{
			DeviceIdentifier:   "dev-1",
			DiscoveredDeviceID: 1,
			Model:              "M60",
			ModelSortValue:     stringPtr("M60"),
		},
		{
			DeviceIdentifier:   "dev-4",
			DiscoveredDeviceID: 4,
		},
	}

	sortDevicePropsForRename(deviceProps, &interfaces.SortConfig{
		Field:     interfaces.SortFieldModel,
		Direction: interfaces.SortDirectionAsc,
	})

	assert.Equal(t, []string{"dev-2", "dev-1", "dev-3", "dev-4"}, []string{
		deviceProps[0].DeviceIdentifier,
		deviceProps[1].DeviceIdentifier,
		deviceProps[2].DeviceIdentifier,
		deviceProps[3].DeviceIdentifier,
	})
}

func TestSortDevicePropsForRename_FirmwareAscPreservesEmptyStringOrdering(t *testing.T) {
	deviceProps := []interfaces.DeviceRenameProperties{
		{
			DeviceIdentifier:   "dev-2",
			DiscoveredDeviceID: 2,
			FirmwareVersion:    "",
			FirmwareSortValue:  stringPtr(""),
		},
		{
			DeviceIdentifier:   "dev-3",
			DiscoveredDeviceID: 3,
			FirmwareVersion:    "Braiins",
			FirmwareSortValue:  stringPtr("Braiins"),
		},
		{
			DeviceIdentifier:   "dev-1",
			DiscoveredDeviceID: 1,
			FirmwareVersion:    "Antminer",
			FirmwareSortValue:  stringPtr("Antminer"),
		},
		{
			DeviceIdentifier:   "dev-4",
			DiscoveredDeviceID: 4,
		},
	}

	sortDevicePropsForRename(deviceProps, &interfaces.SortConfig{
		Field:     interfaces.SortFieldFirmware,
		Direction: interfaces.SortDirectionAsc,
	})

	assert.Equal(t, []string{"dev-2", "dev-1", "dev-3", "dev-4"}, []string{
		deviceProps[0].DeviceIdentifier,
		deviceProps[1].DeviceIdentifier,
		deviceProps[2].DeviceIdentifier,
		deviceProps[3].DeviceIdentifier,
	})
}

func TestValidateRenameNameConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *pb.MinerNameConfig
		wantErr string
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: "name_config is required",
		},
		{
			name: "missing properties",
			config: &pb.MinerNameConfig{
				Separator: "-",
			},
			wantErr: "name_config.properties must contain at least one item",
		},
		{
			name: "invalid separator",
			config: &pb.MinerNameConfig{
				Properties: []*pb.NameProperty{
					{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: "miner"}}},
				},
				Separator: "/",
			},
			wantErr: "name_config.separator must be one of '-', '_', '.', or empty",
		},
		{
			name: "valid config",
			config: &pb.MinerNameConfig{
				Properties: []*pb.NameProperty{
					{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: "miner"}}},
				},
				Separator: "-",
			},
		},
		{
			name: "valid worker-name fixed value",
			config: &pb.MinerNameConfig{
				Properties: []*pb.NameProperty{
					{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_WORKER_NAME}}},
				},
				Separator: "-",
			},
		},
		{
			name: "unsupported fixed value type",
			config: &pb.MinerNameConfig{
				Properties: []*pb.NameProperty{
					{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType(99)}}},
				},
				Separator: "-",
			},
			wantErr: "unsupported fixed value type: 99",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRenameNameConfig(tc.config)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, fleeterror.IsInvalidArgumentError(err))
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestRenameConfigDependsOnDeviceData(t *testing.T) {
	tests := []struct {
		name     string
		config   *pb.MinerNameConfig
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name: "reserved fixed value only",
			config: &pb.MinerNameConfig{
				Properties: []*pb.NameProperty{
					{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_LOCATION}}},
				},
			},
			expected: false,
		},
		{
			name: "reserved fixed value before device dependent property",
			config: &pb.MinerNameConfig{
				Properties: []*pb.NameProperty{
					{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_LOCATION}}},
					{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_SERIAL_NUMBER}}},
				},
			},
			expected: true,
		},
		{
			name: "worker-name fixed value is device dependent",
			config: &pb.MinerNameConfig{
				Properties: []*pb.NameProperty{
					{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_WORKER_NAME}}},
				},
			},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, renameConfigDependsOnDeviceData(tc.config))
		})
	}
}

func TestRenameMiners_RejectsMissingNameConfig(t *testing.T) {
	ctx := authn.SetInfo(context.Background(), &session.Info{
		SessionID:      "test-session-id",
		UserID:         1,
		OrganizationID: 2,
	})
	service := &Service{}

	_, err := service.RenameMiners(ctx, &pb.RenameMinersRequest{})

	require.Error(t, err)
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	require.ErrorContains(t, err, "name_config is required")
}

func TestRenameMiners_RejectsRequestWideGeneratedNameErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := authn.SetInfo(context.Background(), &session.Info{
		SessionID:      "test-session-id",
		UserID:         1,
		OrganizationID: 2,
	})

	deviceStore := storemocks.NewMockDeviceStore(ctrl)
	deviceStore.EXPECT().
		AllDevicesBelongToOrg(gomock.Any(), []string{"dev-1"}, int64(2)).
		Return(true, nil)
	deviceStore.EXPECT().
		GetDevicePropertiesForRename(gomock.Any(), int64(2), []string{"dev-1"}, false).
		Return([]interfaces.DeviceRenameProperties{
			{
				DeviceIdentifier:   "dev-1",
				DiscoveredDeviceID: 1,
			},
		}, nil)

	service := &Service{
		deviceStore:    deviceStore,
		deviceResolver: deviceresolver.New(deviceStore),
	}

	_, err := service.RenameMiners(ctx, &pb.RenameMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonpb.DeviceIdentifierList{
					DeviceIdentifiers: []string{"dev-1"},
				},
			},
		},
		NameConfig: &pb.MinerNameConfig{
			Properties: []*pb.NameProperty{
				{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: strings.Repeat("a", 101)}}},
			},
			Separator: "",
		},
	})

	require.Error(t, err)
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	require.ErrorContains(t, err, "generated name exceeds")
}

func TestRenameMiners_RejectsUnsupportedFixedValueEvenWithDeviceDependentProperties(t *testing.T) {
	ctx := authn.SetInfo(context.Background(), &session.Info{
		SessionID:      "test-session-id",
		UserID:         1,
		OrganizationID: 2,
	})
	service := &Service{}

	_, err := service.RenameMiners(ctx, &pb.RenameMinersRequest{
		NameConfig: &pb.MinerNameConfig{
			Properties: []*pb.NameProperty{
				{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_MODEL}}},
				{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType(99)}}},
			},
			Separator: "-",
		},
	})

	require.Error(t, err)
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	require.ErrorContains(t, err, "unsupported fixed value type: 99")
}

// TestGenerateName_FixedValues verifies each FixedValueType returns the correct device attribute.
func TestGenerateName_FixedValues(t *testing.T) {
	props := baseProps()

	tests := []struct {
		name     string
		fvType   pb.FixedValueType
		expected string
	}{
		{"mac address", pb.FixedValueType_FIXED_VALUE_TYPE_MAC_ADDRESS, props.MacAddress},
		{"serial number", pb.FixedValueType_FIXED_VALUE_TYPE_SERIAL_NUMBER, props.SerialNumber},
		{"worker name", pb.FixedValueType_FIXED_VALUE_TYPE_WORKER_NAME, props.WorkerName},
		{"model", pb.FixedValueType_FIXED_VALUE_TYPE_MODEL, props.Model},
		{"manufacturer", pb.FixedValueType_FIXED_VALUE_TYPE_MANUFACTURER, props.Manufacturer},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &pb.MinerNameConfig{
				Properties: []*pb.NameProperty{
					{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: tc.fvType}}},
				},
				Separator: "-",
			}
			name, err := generateName(cfg, props, 0)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, name)
		})
	}
}

func TestGenerateName_WorkerNameMissingWithOtherSegment(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: "rig"}}},
			{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_WORKER_NAME}}},
		},
		Separator: "-",
	}

	props := baseProps()
	props.WorkerName = ""

	name, err := generateName(cfg, props, 0)
	require.NoError(t, err)
	assert.Equal(t, "rig", name)
}

// TestGenerateName_Separator verifies the separator is placed between segments.
func TestGenerateName_Separator(t *testing.T) {
	tests := []struct {
		sep      string
		expected string
	}{
		{"-", "Bitmain-001"},
		{"_", "Bitmain_001"},
		{".", "Bitmain.001"},
		{"", "Bitmain001"},
	}

	for _, tc := range tests {
		t.Run("sep="+tc.sep, func(t *testing.T) {
			cfg := &pb.MinerNameConfig{
				Properties: []*pb.NameProperty{
					{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_MANUFACTURER}}},
					{Kind: &pb.NameProperty_Counter{Counter: &pb.CounterProperty{CounterStart: 1, CounterScale: 3}}},
				},
				Separator: tc.sep,
			}
			name, err := generateName(cfg, baseProps(), 0)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, name)
		})
	}
}

// TestGenerateName_CharacterCount_First verifies taking the first N characters.
func TestGenerateName_CharacterCount_First(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{
				Type:           pb.FixedValueType_FIXED_VALUE_TYPE_MAC_ADDRESS,
				CharacterCount: int32Ptr(4),
				Section:        sectionPtr(pb.CharacterSection_CHARACTER_SECTION_FIRST),
			}}},
		},
		Separator: "",
	}
	// MAC = "AA:BB:CC:DD:EE:FF", first 4 chars = "AA:B"
	name, err := generateName(cfg, baseProps(), 0)
	require.NoError(t, err)
	assert.Equal(t, "AA:B", name)
}

// TestGenerateName_CharacterCount_Last verifies taking the last N characters.
func TestGenerateName_CharacterCount_Last(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{
				Type:           pb.FixedValueType_FIXED_VALUE_TYPE_MAC_ADDRESS,
				CharacterCount: int32Ptr(5),
				Section:        sectionPtr(pb.CharacterSection_CHARACTER_SECTION_LAST),
			}}},
		},
		Separator: "",
	}
	// MAC = "AA:BB:CC:DD:EE:FF", last 5 chars = "EE:FF"
	name, err := generateName(cfg, baseProps(), 0)
	require.NoError(t, err)
	assert.Equal(t, "EE:FF", name)
}

// TestGenerateName_CharacterCount_Unspecified verifies that CHARACTER_SECTION_UNSPECIFIED
// falls back to taking from the front, matching FIRST behaviour.
func TestGenerateName_CharacterCount_Unspecified(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{
				Type:           pb.FixedValueType_FIXED_VALUE_TYPE_MAC_ADDRESS,
				CharacterCount: int32Ptr(4),
				Section:        sectionPtr(pb.CharacterSection_CHARACTER_SECTION_UNSPECIFIED),
			}}},
		},
		Separator: "",
	}
	// UNSPECIFIED should behave the same as FIRST: "AA:B"
	name, err := generateName(cfg, baseProps(), 0)
	require.NoError(t, err)
	assert.Equal(t, "AA:B", name)
}

// TestGenerateName_CharacterCount_LongerThanValue verifies no truncation when count >= value length.
func TestGenerateName_CharacterCount_LongerThanValue(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{
				Type:           pb.FixedValueType_FIXED_VALUE_TYPE_MODEL,
				CharacterCount: int32Ptr(6),
				Section:        sectionPtr(pb.CharacterSection_CHARACTER_SECTION_FIRST),
			}}},
		},
		Separator: "",
	}
	// Model = "S19Pro" (6 chars) — count == length, returns the full value.
	name, err := generateName(cfg, baseProps(), 0)
	require.NoError(t, err)
	assert.Equal(t, "S19Pro", name)
}

// TestGenerateName_BlankResult verifies a blank name after trim is treated as a no-op.
func TestGenerateName_BlankResult(t *testing.T) {
	// LOCATION is reserved/unimplemented — produces empty segment.
	// With only that property, the joined name is blank.
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_LOCATION}}},
		},
		Separator: "",
	}
	name, err := generateName(cfg, baseProps(), 0)
	require.NoError(t, err)
	assert.Equal(t, "", name)
}

// TestGenerateName_TooLong verifies names exceeding 100 characters return an error.
func TestGenerateName_TooLong(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: strings.Repeat("a", 101)}}},
		},
		Separator: "",
	}
	_, err := generateName(cfg, baseProps(), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

// TestGenerateName_ExactlyMaxLength verifies a 100-character name is accepted.
func TestGenerateName_ExactlyMaxLength(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: strings.Repeat("a", 100)}}},
		},
		Separator: "",
	}
	name, err := generateName(cfg, baseProps(), 0)
	require.NoError(t, err)
	assert.Len(t, name, 100)
}

// TestGenerateName_MultipleProperties verifies all segments are joined with the separator.
func TestGenerateName_MultipleProperties(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_MANUFACTURER}}},
			{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_MODEL}}},
			{Kind: &pb.NameProperty_Counter{Counter: &pb.CounterProperty{CounterStart: 1, CounterScale: 2}}},
		},
		Separator: "-",
	}

	name, err := generateName(cfg, baseProps(), 4)
	require.NoError(t, err)
	assert.Equal(t, "Bitmain-S19Pro-05", name)
}

// TestGenerateName_QualifierReserved verifies qualifier properties produce empty segments
// (BUILDING, RACK, RACK_POSITION are reserved and not yet implemented).
func TestGenerateName_QualifierReserved(t *testing.T) {
	cfg := &pb.MinerNameConfig{
		Properties: []*pb.NameProperty{
			{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: "rig"}}},
			{Kind: &pb.NameProperty_Qualifier{Qualifier: &pb.QualifierProperty{
				Type:   pb.QualifierType_QUALIFIER_TYPE_RACK,
				Prefix: "R",
			}}},
		},
		Separator: "-",
	}
	// Qualifier is reserved → empty segment omitted → only "rig".
	name, err := generateName(cfg, baseProps(), 0)
	require.NoError(t, err)
	assert.Equal(t, "rig", name)
}
