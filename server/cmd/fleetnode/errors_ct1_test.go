package main

import (
	"strconv"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	errorspb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/plugins/mappers"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
)

// A CT1 error carried from earlier evidence claims no last-seen time of its
// own and names when it was last confirmed. Both routes to Fleet must pass
// that through, so the server stamps its own poll time. The simulator cannot
// silence a board, so the report is written out as the plugin produces it.
func TestCarriedCT1ErrorReachesFleetWithoutALastSeenTime(t *testing.T) {
	const deviceID, boardID, confirmed = "ct1-upload", "mac:aa:bb:cc:dd:ee:01", "2026-11-14T09:12:13Z"
	firstSeen := time.Date(2026, 11, 14, 9, 12, 13, 0, time.UTC)
	carried := sdk.DeviceError{
		MinerError: sdkerrors.BoardPowerOvercurrent, Severity: sdkerrors.SeverityCritical, DeviceID: deviceID,
		ComponentType: sdkerrors.ComponentTypeHashBoard, ComponentID: new(boardID),
		CauseSummary: "Board over current",
		Summary:      "Tank 1, module 1, hashboard 1 (aa:bb:cc:dd:ee:01): Board over current (not confirmed or cleared since " + confirmed + ")",
		FirstSeenAt:  firstSeen,
		VendorAttributes: map[string]string{
			"tank": "1", "module": "1", "hashboard_position": "1", "mac_address": "aa:bb:cc:dd:ee:01",
			"vendor_code": "HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT", "ct1_evidence": "retained", "ct1_last_confirmed_at": confirmed,
		},
	}
	reported := sdk.DeviceErrors{DeviceID: deviceID, Errors: []sdk.DeviceError{carried}}

	direct := mappers.SDKDeviceErrorsToFleetDeviceErrors(reported)
	require.Len(t, direct.Errors, 1)
	require.True(t, direct.Errors[0].LastSeenAt.IsZero())
	require.Equal(t, carried.VendorAttributes, direct.Errors[0].VendorAttributes)

	payload, err := getErrorsResultPayload(deviceID, reported)
	require.NoError(t, err)
	result := &pb.GetErrorsResult{}
	require.NoError(t, proto.Unmarshal(payload, result))
	require.NoError(t, protovalidate.Validate(result))
	require.Len(t, result.GetErrors(), 1)
	report := result.GetErrors()[0]
	require.Nil(t, report.GetLastSeenAt())
	require.True(t, firstSeen.Equal(report.GetFirstSeenAt().AsTime()))
	require.Equal(t, errorspb.Severity_SEVERITY_CRITICAL, report.GetSeverity())
	require.Equal(t, boardID, report.GetComponentId())
	require.Equal(t, carried.Summary, report.GetSummary())
	require.Equal(t, carried.VendorAttributes, report.GetVendorAttributes())
}

// requireCT1ErrorPolling follows one CT1 error report from the simulator
// through the plugin RPC and both routes to Fleet: the server's own mapper for
// a directly managed controller, and the Fleet Node result encoder.
func requireCT1ErrorPolling(t *testing.T, device sdk.Device, deviceID, scenario string) {
	t.Helper()
	reported, err := device.GetErrors(t.Context())
	require.NoError(t, err)
	require.Equal(t, deviceID, reported.DeviceID)

	direct := mappers.SDKDeviceErrorsToFleetDeviceErrors(reported)
	require.Len(t, direct.Errors, len(reported.Errors))
	type component struct {
		kind models.ComponentType
		id   string
	}
	components := map[component]map[models.MinerError]models.ErrorMessage{}
	for _, message := range direct.Errors {
		require.Equal(t, deviceID, message.DeviceID)
		require.NotEqual(t, models.MinerErrorUnspecified, message.MinerError)
		require.Contains(t, []models.Severity{models.SeverityCritical, models.SeverityMajor, models.SeverityMinor, models.SeverityInfo}, message.Severity)
		require.Contains(t, []models.ComponentType{models.ComponentTypeHashBoards, models.ComponentTypePSU}, message.ComponentType, "the simulator raises no plant alarms")
		require.NotEmpty(t, message.Summary)
		require.NotEmpty(t, message.VendorCode)
		require.Equal(t, "current", message.VendorAttributes["ct1_evidence"])
		require.NotNil(t, message.ComponentID)
		_, numeric := strconv.Atoi(*message.ComponentID)
		require.Error(t, numeric, "a CT1 component ID is an identity, never a slot number")
		key := component{kind: message.ComponentType, id: *message.ComponentID}
		if components[key] == nil {
			components[key] = map[models.MinerError]models.ErrorMessage{}
		}
		require.NotContains(t, components[key], message.MinerError, "Fleet stores one error per code and component")
		components[key][message.MinerError] = message
	}

	payload, err := getErrorsResultPayload(deviceID, reported)
	require.NoError(t, err)
	require.LessOrEqual(t, len(payload), maxCommandResultPayloadBytes)
	result := &pb.GetErrorsResult{}
	require.NoError(t, proto.Unmarshal(payload, result))
	require.NoError(t, protovalidate.Validate(result), "a report outside the result limits would fail the whole poll")
	require.Equal(t, deviceID, result.GetDeviceId())
	require.Equal(t, len(reported.Errors), len(result.GetErrors())+int(result.GetOmittedReportCount()))
	for _, report := range result.GetErrors() {
		require.NotNil(t, report.GetFirstSeenAt())
		require.NotNil(t, report.GetLastSeenAt())
	}

	psuID := component{kind: models.ComponentTypePSU, id: "tank:2/module:11"}
	switch scenario {
	case "healthy":
		require.Empty(t, reported.Errors)
		require.False(t, result.GetTruncated())
	case "mixed":
		// Tank 2 module 11 has three faulted boards and a hot, faulted PSU;
		// tank 3 module 8 has one faulted board.
		require.False(t, result.GetTruncated())
		require.Len(t, direct.Errors, 8)
		require.Equal(t, models.SeverityMajor, components[psuID][models.PSUFaultGeneric].Severity)
		require.Equal(t, "Tank 2, module 11 PSU: Board power supply", components[psuID][models.PSUFaultGeneric].Summary)
		require.Equal(t, models.SeverityMajor, components[psuID][models.PSUOverTemperature].Severity, "a PSU thermal warning")
		type boardFault struct {
			tank, module, position string
			code                   models.MinerError
		}
		var boards []boardFault
		for _, message := range direct.Errors {
			if message.ComponentType == models.ComponentTypeHashBoards {
				require.Equal(t, models.SeverityCritical, message.Severity)
				attrs := message.VendorAttributes
				boards = append(boards, boardFault{attrs["tank"], attrs["module"], attrs["hashboard_position"], message.MinerError})
			}
		}
		require.ElementsMatch(t, []boardFault{
			{"2", "11", "1", models.HashboardOverTemperature},
			{"2", "11", "2", models.HashboardOverTemperature},
			{"2", "11", "2", models.BoardPowerOvercurrent},
			{"2", "11", "3", models.HashboardOverTemperature},
			{"2", "11", "3", models.BoardPowerOvercurrent},
			{"3", "8", "1", models.BoardPowerOvercurrent},
		}, boards)
	case "all-board-faults":
		// Every board reports every cause: more than one result can carry.
		require.Greater(t, len(reported.Errors), maxGetErrorsReports)
		require.True(t, result.GetTruncated(), "Fleet must keep the errors this result omits")
		require.Len(t, result.GetErrors(), maxGetErrorsReports)
		for _, report := range result.GetErrors() {
			require.Equal(t, errorspb.Severity_SEVERITY_CRITICAL, report.GetSeverity(), "the most severe reports survive truncation")
		}
	default:
		t.Fatalf("unknown scenario %q", scenario)
	}
}
