package rollout_test

import (
	"fmt"
	"strings"
	"testing"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/protovalidate"
	rolloutv1 "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

type observedIdentityValidationCase struct {
	observedName         string
	filterName           string
	manufacturer         string
	model                string
	observedManufacturer string
	observedModel        string
	wantErr              bool
	// filterWantErr marks values that responses may carry but that request
	// filters must reject because stored text cannot represent them.
	filterWantErr bool
}

var observedIdentityValidationCases = []observedIdentityValidationCase{
	{
		observedName: "unknown manufacturer and model are valid",
		filterName:   "empty filters are valid",
	},
	{
		observedName: "unknown manufacturer is valid",
		filterName:   "model-only filter is valid",
		model:        "S21",
	},
	{
		observedName: "unknown model is valid",
		filterName:   "manufacturer-only filter is valid",
		manufacturer: "Bitmain",
	},
	{
		observedName: "canonical identities are valid",
		manufacturer: "Bitmain",
		model:        "S21",
	},
	{
		observedName: "internal printable spaces are valid",
		filterName:   "internal printable spaces are valid",
		manufacturer: "Bit Main",
		model:        "S 21 Pro",
	},
	{
		observedName: "maximum Unicode lengths are valid",
		filterName:   "maximum Unicode lengths are valid",
		manufacturer: strings.Repeat("界", 255),
		model:        strings.Repeat("型", 255),
	},
	{
		observedName:  "whitespace-only manufacturer is valid",
		filterName:    "whitespace-only manufacturer filter is valid",
		manufacturer:  " \t\n",
		observedModel: "S21",
	},
	{
		observedName:         "whitespace-only model is valid",
		filterName:           "whitespace-only model filter is valid",
		model:                " \t\n",
		observedManufacturer: "Bitmain",
	},
	{
		observedName:  "non-ASCII manufacturer is valid",
		filterName:    "non-ASCII manufacturer is valid",
		manufacturer:  "Bítmain",
		observedModel: "S21",
	},
	{
		observedName:         "non-ASCII model is valid",
		filterName:           "non-ASCII model is valid",
		model:                "S２1",
		observedManufacturer: "Bitmain",
	},
	{
		observedName:  "leading manufacturer space is valid",
		filterName:    "leading manufacturer space is valid",
		manufacturer:  " Bitmain",
		observedModel: "S21",
	},
	{
		observedName:  "trailing manufacturer space is valid",
		filterName:    "trailing manufacturer space is valid",
		manufacturer:  "Bitmain ",
		observedModel: "S21",
	},
	{
		observedName:         "leading model space is valid",
		filterName:           "leading model space is valid",
		model:                " S21",
		observedManufacturer: "Bitmain",
	},
	{
		observedName:         "trailing model space is valid",
		filterName:           "trailing model space is valid",
		model:                "S21 ",
		observedManufacturer: "Bitmain",
	},
	{
		observedName:  "internal control character is valid",
		filterName:    "internal control character is valid",
		manufacturer:  "Bit\x01main",
		observedModel: "S21",
	},
	{
		observedName:  "NUL character is valid",
		filterName:    "NUL character is rejected",
		manufacturer:  "Bit\x00main",
		observedModel: "S21",
		filterWantErr: true,
	},
	{
		observedName:  "oversized manufacturer is rejected",
		filterName:    "oversized manufacturer is rejected",
		manufacturer:  strings.Repeat("界", 256),
		observedModel: "S21",
		wantErr:       true,
	},
	{
		observedName:         "oversized model is rejected",
		filterName:           "oversized model is rejected",
		model:                strings.Repeat("型", 256),
		observedManufacturer: "Bitmain",
		wantErr:              true,
	},
}

func (test observedIdentityValidationCase) observedValues() (string, string) {
	manufacturer := test.manufacturer
	if test.observedManufacturer != "" {
		manufacturer = test.observedManufacturer
	}
	model := test.model
	if test.observedModel != "" {
		model = test.observedModel
	}
	return manufacturer, model
}

type deploymentProvenanceValidationCase struct {
	name            string
	response        func(string) proto.Message
	collectionField protoreflect.Name
	fieldNumber     protoreflect.FieldNumber
}

var deploymentProvenanceValidationCases = []deploymentProvenanceValidationCase{
	{
		name: "release channel miners",
		response: func(fileID string) proto.Message {
			return &rolloutv1.ListReleaseChannelMinersResponse{
				Miners: []*rolloutv1.ReleaseChannelMiner{{
					LastDeployedFirmwareFileId: fileID,
				}},
			}
		},
		collectionField: "miners",
		fieldNumber:     7,
	},
	{
		name: "rollout devices",
		response: func(fileID string) proto.Message {
			return &rolloutv1.ListRolloutDevicesResponse{
				Devices: []*rolloutv1.RolloutDevice{{
					LastDeployedFirmwareFileId: fileID,
				}},
			}
		},
		collectionField: "devices",
		fieldNumber:     21,
	},
}

func requireProtoValidation(t *testing.T, message proto.Message, wantErr bool) {
	t.Helper()

	err := protovalidate.Validate(message)
	if wantErr {
		require.Error(t, err)
		return
	}
	require.NoError(t, err)
}

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

			requireProtoValidation(t, test.behavior, test.wantErr)
		})
	}
}

func TestRolloutFirmwareVersionsValidation(t *testing.T) {
	t.Parallel()

	fields := []struct {
		name string
		set  func(*rolloutv1.Rollout, string)
	}{
		{
			name: "target",
			set: func(rollout *rolloutv1.Rollout, version string) {
				rollout.FirmwareFileId = "file-1"
				rollout.FirmwareVersion = version
				rollout.AssignmentGeneration = 1
			},
		},
		{
			name: "lineage",
			set: func(rollout *rolloutv1.Rollout, version string) {
				rollout.PreviousFirmwareFileId = "file-0"
				rollout.PreviousFirmwareVersion = version
			},
		},
	}

	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			t.Parallel()

			newRollout := func(version string) *rolloutv1.Rollout {
				rollout := &rolloutv1.Rollout{
					Manufacturer: "Bitmain",
					Model:        "S21",
				}
				field.set(rollout, version)
				return rollout
			}

			requireProtoValidation(t, newRollout(strings.Repeat("界", 255)), false)
			requireProtoValidation(t, newRollout(strings.Repeat("界", 256)), true)
			requireProtoValidation(t, newRollout("v1\x00custom"), true)
		})
	}
}

func TestRolloutLineageValidation(t *testing.T) {
	t.Parallel()

	newRollout := func(fileID, version, previousFileID, previousVersion string) *rolloutv1.Rollout {
		return &rolloutv1.Rollout{
			Manufacturer:            "Bitmain",
			Model:                   "S21",
			FirmwareFileId:          fileID,
			FirmwareVersion:         version,
			PreviousFirmwareFileId:  previousFileID,
			PreviousFirmwareVersion: previousVersion,
			AssignmentGeneration:    1,
		}
	}
	withoutGeneration := newRollout("file-1", "2.0", "", "")
	withoutGeneration.AssignmentGeneration = 0
	tests := []struct {
		name    string
		rollout *rolloutv1.Rollout
		wantErr bool
	}{
		{name: "first assignment has an empty lineage", rollout: newRollout("file-1", "2.0", "", "")},
		{name: "later assignment records the replaced one", rollout: newRollout("file-1", "2.0", "file-0", "1.0")},
		{name: "untargeted rollout needs no generation", rollout: &rolloutv1.Rollout{Manufacturer: "Bitmain", Model: "S21"}},
		{name: "target file without version is rejected", rollout: newRollout("file-1", "", "", ""), wantErr: true},
		{name: "target version without file is rejected", rollout: newRollout("", "2.0", "", ""), wantErr: true},
		{name: "lineage file without version is rejected", rollout: newRollout("file-1", "2.0", "file-0", ""), wantErr: true},
		{name: "lineage version without file is rejected", rollout: newRollout("file-1", "2.0", "", "1.0"), wantErr: true},
		{name: "lineage equal to the target is rejected", rollout: newRollout("file-1", "2.0", "file-1", "2.0"), wantErr: true},
		{name: "target without assignment generation is rejected", rollout: withoutGeneration, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.rollout, test.wantErr)
		})
	}
}

func TestReleaseChannelModelGroupFirmwareVersionRejectsNUL(t *testing.T) {
	t.Parallel()

	group := &rolloutv1.ReleaseChannelModelGroup{
		FirmwareFileId:             "file-1",
		FirmwareTargetManufacturer: "Bitmain",
		FirmwareTargetModel:        "S21",
		FirmwareVersion:            "v1\x00custom",
		AssignmentGeneration:       1,
	}
	requireProtoValidation(t, group, true)
	group.FirmwareVersion = "v1-custom"
	requireProtoValidation(t, group, false)
}

func TestReleaseChannelModelGroupAssignmentGenerationValidation(t *testing.T) {
	t.Parallel()

	assigned := func(generation int64) *rolloutv1.ReleaseChannelModelGroup {
		return &rolloutv1.ReleaseChannelModelGroup{
			FirmwareFileId:             "file-1",
			FirmwareTargetManufacturer: "Bitmain",
			FirmwareTargetModel:        "S21",
			FirmwareVersion:            "2.0",
			AssignmentGeneration:       generation,
		}
	}
	requireProtoValidation(t, assigned(1), false)
	requireProtoValidation(t, assigned(0), true)
	requireProtoValidation(t, &rolloutv1.ReleaseChannelModelGroup{AssignmentGeneration: 3}, false)
	requireProtoValidation(t, &rolloutv1.ReleaseChannelModelGroup{AssignmentGeneration: -1}, true)
}

func TestRolloutAutomationThresholdsCoverageValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		coverage *float64
		wantErr  bool
	}{
		{name: "unset coverage defaults to full"},
		{name: "full coverage is valid", coverage: proto.Float64(100)},
		{name: "fractional coverage is valid", coverage: proto.Float64(0.5)},
		{name: "zero coverage is rejected", coverage: proto.Float64(0), wantErr: true},
		{name: "coverage above 100 is rejected", coverage: proto.Float64(100.5), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, &rolloutv1.RolloutAutomationThresholds{
				MinSampleCoveragePercent: test.coverage,
			}, test.wantErr)
		})
	}
}

func sampledAggregate(devices int32) *rolloutv1.AggregateMetricComparison {
	return &rolloutv1.AggregateMetricComparison{
		Baseline:       proto.Float64(100),
		Current:        proto.Float64(90),
		SampledDevices: devices,
	}
}

func TestAggregateMetricComparisonValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		aggregate *rolloutv1.AggregateMetricComparison
		wantErr   bool
	}{
		{
			name:      "absent aggregate samples no miners",
			aggregate: &rolloutv1.AggregateMetricComparison{},
		},
		{
			name:      "sampled aggregate carries both halves",
			aggregate: sampledAggregate(3),
		},
		{
			name: "sampled aggregate missing current is rejected",
			aggregate: &rolloutv1.AggregateMetricComparison{
				Baseline:       proto.Float64(100),
				SampledDevices: 1,
			},
			wantErr: true,
		},
		{
			name: "sampled aggregate missing baseline is rejected",
			aggregate: &rolloutv1.AggregateMetricComparison{
				Current:        proto.Float64(90),
				SampledDevices: 1,
			},
			wantErr: true,
		},
		{
			name:      "both halves without samples are rejected",
			aggregate: sampledAggregate(0),
			wantErr:   true,
		},
		{
			name:      "one half without samples is rejected",
			aggregate: &rolloutv1.AggregateMetricComparison{Baseline: proto.Float64(100)},
			wantErr:   true,
		},
		{
			name:      "negative sample count is rejected",
			aggregate: &rolloutv1.AggregateMetricComparison{SampledDevices: -1},
			wantErr:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.aggregate, test.wantErr)
		})
	}
}

func TestRolloutEvidenceValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		evidence *rolloutv1.RolloutEvidence
		wantErr  bool
	}{
		{
			name:     "empty evidence is valid",
			evidence: &rolloutv1.RolloutEvidence{},
		},
		{
			name: "consistent evidence is valid",
			evidence: &rolloutv1.RolloutEvidence{
				DevicesTotal:             4,
				Verified:                 3,
				Failed:                   1,
				Online:                   4,
				Hashing:                  3,
				BaselineHashing:          4,
				HashRateHs:               sampledAggregate(3),
				TempC:                    sampledAggregate(2),
				HashrateChangePercent:    proto.Float64(-10),
				TemperatureChangeCelsius: proto.Float64(-10),
				HoldReason:               strings.Repeat("h", 1024),
			},
		},
		{
			name: "zero baseline leaves the percent change unset",
			evidence: &rolloutv1.RolloutEvidence{
				DevicesTotal: 1,
				Verified:     1,
				HashRateHs: &rolloutv1.AggregateMetricComparison{
					Baseline:       proto.Float64(0),
					Current:        proto.Float64(50),
					SampledDevices: 1,
				},
			},
		},
		{
			name: "percent change with a zero baseline is rejected",
			evidence: &rolloutv1.RolloutEvidence{
				DevicesTotal: 1,
				Verified:     1,
				EfficiencyJh: &rolloutv1.AggregateMetricComparison{
					Baseline:       proto.Float64(0),
					Current:        proto.Float64(50),
					SampledDevices: 1,
				},
				EfficiencyChangePercent: proto.Float64(100),
			},
			wantErr: true,
		},
		{
			name:     "sampled hashrate without its change is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, HashRateHs: sampledAggregate(1)},
			wantErr:  true,
		},
		{
			name:     "sampled temperature without its change is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, TempC: sampledAggregate(1)},
			wantErr:  true,
		},
		{
			name:     "verified plus failed above total is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 3, Verified: 3, Failed: 1},
			wantErr:  true,
		},
		{
			name:     "online above total is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Online: 2},
			wantErr:  true,
		},
		{
			name:     "hashing above total is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Hashing: 2},
			wantErr:  true,
		},
		{
			name:     "baseline hashing above total is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, BaselineHashing: 2},
			wantErr:  true,
		},
		{
			name:     "aggregate sampling more than verified is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 3, Verified: 1, PowerW: sampledAggregate(2)},
			wantErr:  true,
		},
		{
			name:     "hashrate change without samples is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, HashrateChangePercent: proto.Float64(0)},
			wantErr:  true,
		},
		{
			name:     "efficiency change without samples is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, EfficiencyChangePercent: proto.Float64(0)},
			wantErr:  true,
		},
		{
			name:     "temperature change without samples is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, TemperatureChangeCelsius: proto.Float64(0)},
			wantErr:  true,
		},
		{
			name:     "negative count is rejected",
			evidence: &rolloutv1.RolloutEvidence{NewErrors: -1},
			wantErr:  true,
		},
		{
			name:     "oversized hold reason is rejected",
			evidence: &rolloutv1.RolloutEvidence{HoldReason: strings.Repeat("h", 1025)},
			wantErr:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.evidence, test.wantErr)
		})
	}
}

// Every string carries a max_len and every list a max_items so no payload in
// the contract can grow without an explicit limit.
func TestEveryContractFieldIsBounded(t *testing.T) {
	t.Parallel()

	var visit func(messages protoreflect.MessageDescriptors)
	visit = func(messages protoreflect.MessageDescriptors) {
		for i := range messages.Len() {
			message := messages.Get(i)
			visit(message.Messages())
			fields := message.Fields()
			for j := range fields.Len() {
				field := fields.Get(j)
				rules, _ := proto.GetExtension(field.Options(), validate.E_Field).(*validate.FieldRules)
				stringRules := rules.GetString_()
				if field.IsList() {
					require.Positivef(t, rules.GetRepeated().GetMaxItems(), "%s must set max_items", field.FullName())
					stringRules = rules.GetRepeated().GetItems().GetString_()
				}
				if field.Kind() == protoreflect.StringKind {
					require.Positivef(t, stringRules.GetMaxLen(), "%s must set max_len", field.FullName())
				}
			}
		}
	}
	visit(rolloutv1.File_rollout_v1_rollout_proto.Messages())
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
			name: "surrounding whitespace in an assignment target is rejected",
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
			name: "distinct manufacturer and model pairs are accepted",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: "bitMAIN", Model: "S19", FirmwareFileId: "firmware-b"},
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
		{
			name: "maximum firmware file id is accepted",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{{
					Manufacturer:   "Bitmain",
					Model:          "S21",
					FirmwareFileId: strings.Repeat("f", 255),
				}},
			},
		},
		{
			name: "oversized firmware file id is rejected",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{{
					Manufacturer:   "Bitmain",
					Model:          "S21",
					FirmwareFileId: strings.Repeat("f", 256),
				}},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.request, test.wantErr)
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
				Manufacturer:         "Bitmain",
				Model:                "S21",
				ReportedVersions:     boundedVersions,
				ReportedVersionCount: 12,
			},
		},
		{
			name: "oversized list is rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				ReportedVersions:     oversizedVersions,
				ReportedVersionCount: 11,
			},
			wantErr: true,
		},
		{
			name: "count below returned list length is rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				ReportedVersions:     []string{"1.0.0", "2.0.0"},
				ReportedVersionCount: 1,
			},
			wantErr: true,
		},
		{
			name: "duplicate versions are rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				ReportedVersions:     []string{"1.0.0", "1.0.0"},
				ReportedVersionCount: 2,
			},
			wantErr: true,
		},
		{
			name: "oversized version is rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				ReportedVersions:     []string{strings.Repeat("v", 256)},
				ReportedVersionCount: 1,
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.modelGroup, test.wantErr)
		})
	}
}

func TestReleaseChannelModelGroupAssignmentValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		modelGroup *rolloutv1.ReleaseChannelModelGroup
		wantErr    bool
	}{
		{
			name:       "unassigned unknown identity is valid",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{},
		},
		{
			name: "unassigned observed identity may be noncanonical",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer: "Bít main ",
				Model:        " S２1\t",
			},
		},
		{
			name: "complete assignment with noncanonical observed identity is valid",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:               "Bít main ",
				Model:                      " S２1\t",
				FirmwareFileId:             "firmware",
				FirmwareVersion:            "1.0.0",
				FirmwareTargetManufacturer: "Bitmain",
				FirmwareTargetModel:        "S21",
				AssignmentGeneration:       1,
			},
		},
		{
			name: "assignment fields without firmware file are rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				FirmwareVersion:            "1.0.0",
				FirmwareTargetManufacturer: "Bitmain",
				FirmwareTargetModel:        "S21",
			},
			wantErr: true,
		},
		{
			name: "assigned firmware requires target manufacturer",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:        "Bitmain",
				Model:               "S21",
				FirmwareFileId:      "firmware",
				FirmwareVersion:     "1.0.0",
				FirmwareTargetModel: "S21",
			},
			wantErr: true,
		},
		{
			name: "assigned firmware requires target model",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:               "Bitmain",
				Model:                      "S21",
				FirmwareFileId:             "firmware",
				FirmwareVersion:            "1.0.0",
				FirmwareTargetManufacturer: "Bitmain",
			},
			wantErr: true,
		},
		{
			name: "assigned firmware requires version",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:               "Bitmain",
				Model:                      "S21",
				FirmwareFileId:             "firmware",
				FirmwareTargetManufacturer: "Bitmain",
				FirmwareTargetModel:        "S21",
			},
			wantErr: true,
		},
		{
			name: "assigned firmware requires canonical target manufacturer",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:               "Bitmain",
				Model:                      "S21",
				FirmwareFileId:             "firmware",
				FirmwareVersion:            "1.0.0",
				FirmwareTargetManufacturer: "Bítmain",
				FirmwareTargetModel:        "S21",
			},
			wantErr: true,
		},
		{
			name: "assigned firmware requires canonical target model",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:               "Bitmain",
				Model:                      "S21",
				FirmwareFileId:             "firmware",
				FirmwareVersion:            "1.0.0",
				FirmwareTargetManufacturer: "Bitmain",
				FirmwareTargetModel:        " S21 ",
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.modelGroup, test.wantErr)
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
		{
			name:    "channel model groups accept default page size",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1},
		},
		{
			name:    "channel model groups accept maximum page",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1, PageSize: 100},
		},
		{
			name:    "channel model groups reject negative page",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1, PageSize: -1},
			wantErr: true,
		},
		{
			name:    "channel model groups reject oversized page",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1, PageSize: 101},
			wantErr: true,
		},
		{
			name:    "channel model groups accept maximum cursor length",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1, Cursor: strings.Repeat("c", 8192)},
		},
		{
			name:    "channel model groups reject oversized cursor",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1, Cursor: strings.Repeat("c", 8193)},
			wantErr: true,
		},
		{
			name:    "membership conflicts accept organization-wide default page",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{},
		},
		{
			name:    "membership conflicts accept channel filter and maximum page",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{ChannelId: 1, PageSize: 100},
		},
		{
			name:    "membership conflicts reject negative channel filter",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{ChannelId: -1},
			wantErr: true,
		},
		{
			name:    "membership conflicts reject negative page",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{PageSize: -1},
			wantErr: true,
		},
		{
			name:    "membership conflicts reject oversized page",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{PageSize: 101},
			wantErr: true,
		},
		{
			name:    "membership conflicts accept maximum cursor length",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{Cursor: strings.Repeat("c", 100)},
		},
		{
			name:    "membership conflicts reject oversized cursor",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{Cursor: strings.Repeat("c", 101)},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.request, test.wantErr)
		})
	}
}

func TestOptionalReleaseChannelIDValidation(t *testing.T) {
	t.Parallel()

	requests := []struct {
		name string
		new  func(channelID int64) proto.Message
	}{
		{
			name: "scope preview",
			new: func(channelID int64) proto.Message {
				return &rolloutv1.PreviewReleaseChannelScopeRequest{ChannelId: channelID}
			},
		},
		{
			name: "rollout list",
			new: func(channelID int64) proto.Message {
				return &rolloutv1.ListRolloutsRequest{ChannelId: channelID}
			},
		},
	}
	tests := []struct {
		name      string
		channelID int64
		wantErr   bool
	}{
		{name: "zero is valid"},
		{name: "positive is valid", channelID: 1},
		{name: "negative is rejected", channelID: -1, wantErr: true},
	}

	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			t.Parallel()

			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()

					requireProtoValidation(t, request.new(test.channelID), test.wantErr)
				})
			}
		})
	}
}

func TestBoundedListResponseValidation(t *testing.T) {
	t.Parallel()

	newRollout := func() proto.Message {
		return &rolloutv1.Rollout{Manufacturer: "Bitmain", Model: "S21"}
	}
	tests := []struct {
		name            string
		newResponse     func() proto.Message
		newElement      func() proto.Message
		collectionField protoreflect.Name
		maxItems        int
		cursorMaxLen    int
	}{
		{
			name:            "release channels",
			newResponse:     func() proto.Message { return &rolloutv1.ListReleaseChannelsResponse{} },
			newElement:      func() proto.Message { return &rolloutv1.ReleaseChannelSummary{} },
			collectionField: "channels",
			maxItems:        1000,
			cursorMaxLen:    100,
		},
		{
			name:            "release channel miners",
			newResponse:     func() proto.Message { return &rolloutv1.ListReleaseChannelMinersResponse{} },
			newElement:      func() proto.Message { return &rolloutv1.ReleaseChannelMiner{} },
			collectionField: "miners",
			maxItems:        1000,
			cursorMaxLen:    100,
		},
		{
			name:            "rollouts",
			newResponse:     func() proto.Message { return &rolloutv1.ListRolloutsResponse{} },
			newElement:      newRollout,
			collectionField: "rollouts",
			maxItems:        1000,
			cursorMaxLen:    100,
		},
		{
			name:            "rollout devices",
			newResponse:     func() proto.Message { return &rolloutv1.ListRolloutDevicesResponse{} },
			newElement:      func() proto.Message { return &rolloutv1.RolloutDevice{} },
			collectionField: "devices",
			maxItems:        1000,
			cursorMaxLen:    100,
		},
		{
			name:            "applied rollouts",
			newResponse:     func() proto.Message { return &rolloutv1.ApplyReleaseChannelFirmwareResponse{} },
			newElement:      newRollout,
			collectionField: "started_rollouts",
			maxItems:        100,
		},
		{
			name:            "rollback rollout",
			newResponse:     func() proto.Message { return &rolloutv1.RollbackReleaseChannelFirmwareResponse{} },
			newElement:      newRollout,
			collectionField: "started_rollouts",
			maxItems:        1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response := test.newResponse()
			responseMessage := response.ProtoReflect()
			collection := responseMessage.Mutable(
				responseMessage.Descriptor().Fields().ByName(test.collectionField),
			).List()
			for range test.maxItems {
				collection.Append(protoreflect.ValueOfMessage(test.newElement().ProtoReflect()))
			}
			requireProtoValidation(t, response, false)

			collection.Append(protoreflect.ValueOfMessage(test.newElement().ProtoReflect()))
			requireProtoValidation(t, response, true)

			if test.cursorMaxLen == 0 {
				return
			}
			response = test.newResponse()
			responseMessage = response.ProtoReflect()
			cursor := responseMessage.Descriptor().Fields().ByName("cursor")
			responseMessage.Set(cursor, protoreflect.ValueOfString(strings.Repeat("c", test.cursorMaxLen)))
			requireProtoValidation(t, response, false)
			responseMessage.Set(cursor, protoreflect.ValueOfString(strings.Repeat("c", test.cursorMaxLen+1)))
			requireProtoValidation(t, response, true)
		})
	}
}

func TestListReleaseChannelMembershipConflictsResponseValidation(t *testing.T) {
	t.Parallel()

	conflicts := make([]*rolloutv1.ReleaseChannelMembershipConflict, 101)
	for index := range conflicts {
		conflicts[index] = &rolloutv1.ReleaseChannelMembershipConflict{
			DeviceId:            int64(index + 1),
			DeviceIdentifier:    fmt.Sprintf("device-%d", index),
			Manufacturer:        "Bitmain",
			Model:               "S21",
			ChannelId:           int64(index + 1),
			ChannelName:         fmt.Sprintf("channel-%d", index),
			SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
			Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
		}
	}

	tests := []struct {
		name     string
		response *rolloutv1.ListReleaseChannelMembershipConflictsResponse
		wantErr  bool
	}{
		{
			name: "maximum page and cursor length are valid",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: conflicts[:100],
				Cursor:    strings.Repeat("c", 100),
			},
		},
		{
			name: "winner resolution is valid",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
		},
		{
			name: "loser resolution is valid",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_SITE,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_LOSER,
				}},
			},
		},
		{
			name: "excluded tie resolution is valid",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_RACK,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_EXCLUDED_TIE,
				}},
			},
		},
		{
			name: "oversized page is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: conflicts,
			},
			wantErr: true,
		},
		{
			name: "oversized cursor is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Cursor: strings.Repeat("c", 101),
			},
			wantErr: true,
		},
		{
			name: "oversized device identifier is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					DeviceIdentifier:    strings.Repeat("d", 256),
					Manufacturer:        "Bitmain",
					Model:               "S21",
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "oversized manufacturer is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        strings.Repeat("m", 256),
					Model:               "S21",
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "oversized model is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        "Bitmain",
					Model:               strings.Repeat("m", 256),
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "oversized channel name is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        "Bitmain",
					Model:               "S21",
					ChannelName:         strings.Repeat("c", 101),
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "unknown selector specificity is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        "Bitmain",
					Model:               "S21",
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity(99),
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "unspecified selector specificity is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer: "Bitmain",
					Model:        "S21",
					Resolution:   rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "unknown resolution is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        "Bitmain",
					Model:               "S21",
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution(99),
				}},
			},
			wantErr: true,
		},
		{
			name: "unspecified resolution is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        "Bitmain",
					Model:               "S21",
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
				}},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.response, test.wantErr)
		})
	}
}

func TestListReleaseChannelModelGroupsResponseValidation(t *testing.T) {
	t.Parallel()

	modelGroups := make([]*rolloutv1.ReleaseChannelModelGroup, 101)
	for index := range modelGroups {
		modelGroups[index] = &rolloutv1.ReleaseChannelModelGroup{
			Manufacturer: "Bitmain",
			Model:        fmt.Sprintf("model-%d", index),
		}
	}

	requireProtoValidation(t, &rolloutv1.ListReleaseChannelModelGroupsResponse{
		ModelGroups: modelGroups[:100],
	}, false)
	requireProtoValidation(t, &rolloutv1.ListReleaseChannelModelGroupsResponse{
		ModelGroups: modelGroups,
	}, true)
	requireProtoValidation(t, &rolloutv1.ListReleaseChannelModelGroupsResponse{
		Cursor: strings.Repeat("c", 8192),
	}, false)
	requireProtoValidation(t, &rolloutv1.ListReleaseChannelModelGroupsResponse{
		Cursor: strings.Repeat("c", 8193),
	}, true)
}

func TestPreviewReleaseChannelScopeResponseValidation(t *testing.T) {
	t.Parallel()

	models := make([]*rolloutv1.ReleaseChannelScopeModelCount, 101)
	conflicts := make([]*rolloutv1.ReleaseChannelScopeConflict, 101)
	for index := range models {
		models[index] = &rolloutv1.ReleaseChannelScopeModelCount{
			Manufacturer: "manufacturer",
			Model:        fmt.Sprintf("model-%d", index),
		}
		conflicts[index] = &rolloutv1.ReleaseChannelScopeConflict{
			ChannelId:   int64(index + 1),
			ChannelName: fmt.Sprintf("channel-%d", index),
		}
	}

	tests := []struct {
		name     string
		response *rolloutv1.PreviewReleaseChannelScopeResponse
		wantErr  bool
	}{
		{
			name: "bounded truncated results are valid",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models:        models[:100],
				Conflicts:     conflicts[:100],
				ModelCount:    101,
				ConflictCount: 101,
			},
		},
		{
			name: "complete results below the limit are valid",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models:        models[:2],
				Conflicts:     conflicts[:2],
				ModelCount:    2,
				ConflictCount: 2,
			},
		},
		{
			name: "101 models are rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models:     models,
				ModelCount: 101,
			},
			wantErr: true,
		},
		{
			name: "101 conflicts are rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Conflicts:     conflicts,
				ConflictCount: 101,
			},
			wantErr: true,
		},
		{
			name: "model count below returned list length is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models:     models[:2],
				ModelCount: 1,
			},
			wantErr: true,
		},
		{
			name: "conflict count below returned list length is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Conflicts:     conflicts[:2],
				ConflictCount: 1,
			},
			wantErr: true,
		},
		{
			name: "underfilled model list is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models:     models[:1],
				ModelCount: 2,
			},
			wantErr: true,
		},
		{
			name: "underfilled conflict list is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Conflicts:     conflicts[:1],
				ConflictCount: 2,
			},
			wantErr: true,
		},
		{
			name: "oversized manufacturer is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models: []*rolloutv1.ReleaseChannelScopeModelCount{{
					Manufacturer: strings.Repeat("m", 256),
					Model:        "S21",
				}},
				ModelCount: 1,
			},
			wantErr: true,
		},
		{
			name: "oversized model is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models: []*rolloutv1.ReleaseChannelScopeModelCount{{
					Manufacturer: "Bitmain",
					Model:        strings.Repeat("m", 256),
				}},
				ModelCount: 1,
			},
			wantErr: true,
		},
		{
			name: "oversized conflict channel name is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Conflicts: []*rolloutv1.ReleaseChannelScopeConflict{{
					ChannelId:   1,
					ChannelName: strings.Repeat("c", 101),
				}},
				ConflictCount: 1,
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.response, test.wantErr)
		})
	}
}

func TestDeploymentProvenanceResponseDescriptors(t *testing.T) {
	t.Parallel()

	for _, test := range deploymentProvenanceValidationCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			items := test.response("").ProtoReflect().Descriptor().Fields().ByName(test.collectionField)
			require.NotNil(t, items)
			require.True(t, items.IsList())

			provenance := items.Message().Fields().ByName("last_deployed_firmware_file_id")
			require.NotNil(t, provenance)
			require.Equal(t, test.fieldNumber, provenance.Number())
			require.Equal(t, protoreflect.StringKind, provenance.Kind())
		})
	}
}

func TestDeploymentProvenanceResponseValidation(t *testing.T) {
	t.Parallel()

	for _, test := range deploymentProvenanceValidationCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.response(strings.Repeat("f", 255)), false)
			requireProtoValidation(t, test.response(strings.Repeat("f", 256)), true)
		})
	}
}

func TestRolloutDetailsArePagedSeparately(t *testing.T) {
	t.Parallel()

	rolloutFields := (&rolloutv1.Rollout{}).ProtoReflect().Descriptor().Fields()
	require.Nil(t, rolloutFields.ByName("devices"))
	require.NotNil(t, rolloutFields.ByName("device_count"))

	modelGroup := (&rolloutv1.ReleaseChannelModelGroup{}).ProtoReflect().Descriptor()
	modelGroupFields := modelGroup.Fields()
	require.Nil(t, modelGroupFields.ByName("miners"))
	require.NotNil(t, modelGroupFields.ByName("miner_count"))

	channelSummary := (&rolloutv1.ReleaseChannelSummary{}).ProtoReflect().Descriptor()
	require.Nil(t, channelSummary.Fields().ByName("scope"))
	require.Nil(t, channelSummary.Fields().ByName("model_groups"))
	require.NotNil(t, channelSummary.Fields().ByName("model_group_count"))

	channel := (&rolloutv1.ReleaseChannel{}).ProtoReflect().Descriptor()
	require.Nil(t, channel.Fields().ByName("model_groups"))
	require.NotNil(t, channel.Fields().ByName("model_group_count"))

	methods := rolloutv1.File_rollout_v1_rollout_proto.
		Services().
		ByName(protoreflect.Name("RolloutService")).
		Methods()
	listReleaseChannels := methods.ByName("ListReleaseChannels")
	require.NotNil(t, listReleaseChannels)
	channelsField := listReleaseChannels.Output().Fields().ByName("channels")
	require.NotNil(t, channelsField)
	require.Equal(t, channelSummary.FullName(), channelsField.Message().FullName())

	listReleaseChannelModelGroups := methods.ByName("ListReleaseChannelModelGroups")
	require.NotNil(t, listReleaseChannelModelGroups)
	modelGroupsField := listReleaseChannelModelGroups.Output().Fields().ByName("model_groups")
	require.NotNil(t, modelGroupsField)
	require.True(t, modelGroupsField.IsList())
	require.Equal(t, modelGroup.FullName(), modelGroupsField.Message().FullName())
	require.NotNil(t, listReleaseChannelModelGroups.Output().Fields().ByName("cursor"))

	require.NotNil(t, methods.ByName("ListReleaseChannelMiners"))
	require.NotNil(t, methods.ByName("ListRolloutDevices"))
}

func TestListReleaseChannelMembershipConflictsDescriptor(t *testing.T) {
	t.Parallel()

	method := rolloutv1.File_rollout_v1_rollout_proto.
		Services().
		ByName(protoreflect.Name("RolloutService")).
		Methods().
		ByName("ListReleaseChannelMembershipConflicts")
	require.NotNil(t, method)

	request := (&rolloutv1.ListReleaseChannelMembershipConflictsRequest{}).ProtoReflect().Descriptor()
	response := (&rolloutv1.ListReleaseChannelMembershipConflictsResponse{}).ProtoReflect().Descriptor()
	conflict := (&rolloutv1.ReleaseChannelMembershipConflict{}).ProtoReflect().Descriptor()

	require.Equal(t, request.FullName(), method.Input().FullName())
	require.Equal(t, response.FullName(), method.Output().FullName())
	require.NotNil(t, request.Fields().ByName("channel_id"))
	require.NotNil(t, request.Fields().ByName("page_size"))
	require.NotNil(t, request.Fields().ByName("cursor"))

	conflictsField := response.Fields().ByName("conflicts")
	require.NotNil(t, conflictsField)
	require.True(t, conflictsField.IsList())
	require.Equal(t, conflict.FullName(), conflictsField.Message().FullName())
	require.NotNil(t, response.Fields().ByName("cursor"))

	specificityField := conflict.Fields().ByName("selector_specificity")
	require.NotNil(t, specificityField)
	require.Equal(t, protoreflect.FullName("rollout.v1.ReleaseChannelSelectorSpecificity"), specificityField.Enum().FullName())

	resolutionField := conflict.Fields().ByName("resolution")
	require.NotNil(t, resolutionField)
	resolutionEnum := resolutionField.Enum()
	require.Equal(t, protoreflect.FullName("rollout.v1.ReleaseChannelConflictResolution"), resolutionEnum.FullName())
	require.NotNil(t, resolutionEnum.Values().ByName("RELEASE_CHANNEL_CONFLICT_RESOLUTION_UNSPECIFIED"))
	require.NotNil(t, resolutionEnum.Values().ByName("RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER"))
	require.NotNil(t, resolutionEnum.Values().ByName("RELEASE_CHANNEL_CONFLICT_RESOLUTION_LOSER"))
	require.NotNil(t, resolutionEnum.Values().ByName("RELEASE_CHANNEL_CONFLICT_RESOLUTION_EXCLUDED_TIE"))

	options, ok := method.Options().(*descriptorpb.MethodOptions)
	require.True(t, ok)
	require.Equal(t, descriptorpb.MethodOptions_NO_SIDE_EFFECTS, options.GetIdempotencyLevel())
}

func TestRequiredManufacturerModelTargetKeyValidation(t *testing.T) {
	t.Parallel()

	messages := []struct {
		name string
		new  func(manufacturer, model string) proto.Message
	}{
		{
			name: "firmware assignment",
			new: func(manufacturer, model string) proto.Message {
				return &rolloutv1.FirmwareAssignment{Manufacturer: manufacturer, Model: model}
			},
		},
		{
			name: "rollout",
			new: func(manufacturer, model string) proto.Message {
				return &rolloutv1.Rollout{Manufacturer: manufacturer, Model: model}
			},
		},
	}
	tests := []struct {
		name         string
		manufacturer string
		model        string
		wantErr      bool
	}{
		{name: "simple keys are valid", manufacturer: "Bitmain", model: "S21"},
		{name: "internal printable spaces are valid", manufacturer: "Bit Main", model: "S 21 Pro"},
		{name: "maximum lengths are valid", manufacturer: strings.Repeat("m", 255), model: strings.Repeat("n", 255)},
		{name: "empty manufacturer is rejected", model: "S21", wantErr: true},
		{name: "empty model is rejected", manufacturer: "Bitmain", wantErr: true},
		{name: "whitespace-only manufacturer is rejected", manufacturer: " \t\n", model: "S21", wantErr: true},
		{name: "whitespace-only model is rejected", manufacturer: "Bitmain", model: " \t\n", wantErr: true},
		{name: "non-ASCII manufacturer is rejected", manufacturer: "Bítmain", model: "S21", wantErr: true},
		{name: "non-ASCII model is rejected", manufacturer: "Bitmain", model: "S２1", wantErr: true},
		{name: "leading manufacturer space is rejected", manufacturer: " Bitmain", model: "S21", wantErr: true},
		{name: "trailing manufacturer space is rejected", manufacturer: "Bitmain ", model: "S21", wantErr: true},
		{name: "leading model space is rejected", manufacturer: "Bitmain", model: " S21", wantErr: true},
		{name: "trailing model space is rejected", manufacturer: "Bitmain", model: "S21 ", wantErr: true},
		{name: "internal control whitespace is rejected", manufacturer: "Bit\tmain", model: "S21", wantErr: true},
		{name: "oversized manufacturer is rejected", manufacturer: strings.Repeat("m", 256), model: "S21", wantErr: true},
		{name: "oversized model is rejected", manufacturer: "Bitmain", model: strings.Repeat("m", 256), wantErr: true},
	}

	for _, message := range messages {
		t.Run(message.name, func(t *testing.T) {
			t.Parallel()

			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()

					requireProtoValidation(t, message.new(test.manufacturer, test.model), test.wantErr)
				})
			}
		})
	}
}

func TestObservedManufacturerModelIdentityValidation(t *testing.T) {
	t.Parallel()

	messages := []struct {
		name string
		new  func(manufacturer, model string) proto.Message
	}{
		{
			name: "model group",
			new: func(manufacturer, model string) proto.Message {
				return &rolloutv1.ReleaseChannelModelGroup{Manufacturer: manufacturer, Model: model}
			},
		},
		{
			name: "channel miner",
			new: func(manufacturer, model string) proto.Message {
				return &rolloutv1.ReleaseChannelMiner{Manufacturer: manufacturer, Model: model}
			},
		},
		{
			name: "membership conflict",
			new: func(manufacturer, model string) proto.Message {
				return &rolloutv1.ReleaseChannelMembershipConflict{
					Manufacturer:        manufacturer,
					Model:               model,
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}
			},
		},
		{
			name: "scope model count",
			new: func(manufacturer, model string) proto.Message {
				return &rolloutv1.ReleaseChannelScopeModelCount{Manufacturer: manufacturer, Model: model}
			},
		},
	}
	for _, message := range messages {
		t.Run(message.name, func(t *testing.T) {
			t.Parallel()

			for _, test := range observedIdentityValidationCases {
				t.Run(test.observedName, func(t *testing.T) {
					t.Parallel()

					manufacturer, model := test.observedValues()
					requireProtoValidation(
						t,
						message.new(manufacturer, model),
						test.wantErr,
					)
				})
			}
		})
	}
}

func TestOptionalManufacturerModelFilterValidation(t *testing.T) {
	t.Parallel()

	for _, test := range observedIdentityValidationCases {
		if test.filterName == "" {
			continue
		}

		t.Run(test.filterName, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, &rolloutv1.ListReleaseChannelMinersRequest{
				ChannelId:    1,
				Manufacturer: test.manufacturer,
				Model:        test.model,
			}, test.wantErr || test.filterWantErr)
		})
	}
}

func TestPersistedRequestStringsRejectNUL(t *testing.T) {
	t.Parallel()

	fields := []struct {
		name string
		new  func(value string) proto.Message
	}{
		{
			name: "scope device identifier",
			new: func(value string) proto.Message {
				return &rolloutv1.ReleaseChannelScope{DeviceIdentifiers: []string{value}}
			},
		},
		{
			name: "create channel name",
			new: func(value string) proto.Message {
				return &rolloutv1.CreateReleaseChannelRequest{Name: value}
			},
		},
		{
			name: "create channel description",
			new: func(value string) proto.Message {
				return &rolloutv1.CreateReleaseChannelRequest{Name: "stable", Description: value}
			},
		},
		{
			name: "update channel name",
			new: func(value string) proto.Message {
				return &rolloutv1.UpdateReleaseChannelRequest{ChannelId: 1, Name: value}
			},
		},
		{
			name: "update channel description",
			new: func(value string) proto.Message {
				return &rolloutv1.UpdateReleaseChannelRequest{ChannelId: 1, Name: "stable", Description: value}
			},
		},
	}

	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, field.new("miner-01"), false)
			requireProtoValidation(t, field.new("miner\x0001"), true)
		})
	}
}

func TestRolloutDeviceLastErrorValidation(t *testing.T) {
	t.Parallel()

	requireProtoValidation(t, &rolloutv1.RolloutDevice{LastError: strings.Repeat("e", 2048)}, false)
	requireProtoValidation(t, &rolloutv1.RolloutDevice{LastError: strings.Repeat("e", 2049)}, true)
}
