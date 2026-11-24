package ct1

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// alarmServer is a single controller that counts plant alarm commands.
type alarmServer struct {
	mdk.UnimplementedSystemServiceServer
	mdk.UnimplementedPowerServiceServer
	mdk.UnimplementedTelemetryServiceServer
	controls    *mdk.GetContainerControlsResponse
	controlsErr error
	health      *mdk.GetContainerHealthResponse
	healthErr   error
	writeErr    error
	mutes       atomic.Int32
	resets      atomic.Int32
}

func (s *alarmServer) GetSystemInfo(context.Context, *mdk.GetSystemInfoRequest) (*mdk.GetSystemInfoResponse, error) {
	return &mdk.GetSystemInfoResponse{Info: &mdk.SystemInfo{Id: "ct1", Product: "CT1", Version: "test"}}, nil
}

func (s *alarmServer) GetContainerControls(context.Context, *mdk.GetContainerControlsRequest) (*mdk.GetContainerControlsResponse, error) {
	return s.controls, s.controlsErr
}

func (s *alarmServer) GetContainerHealth(context.Context, *mdk.GetContainerHealthRequest) (*mdk.GetContainerHealthResponse, error) {
	return s.health, s.healthErr
}

func (s *alarmServer) MuteContainerAlarm(context.Context, *mdk.MuteContainerAlarmRequest) (*mdk.MuteContainerAlarmResponse, error) {
	s.mutes.Add(1)
	return &mdk.MuteContainerAlarmResponse{}, s.writeErr
}

func (s *alarmServer) ResetContainerAlarm(context.Context, *mdk.ResetContainerAlarmRequest) (*mdk.ResetContainerAlarmResponse, error) {
	s.resets.Add(1)
	return &mdk.ResetContainerAlarmResponse{}, s.writeErr
}

func (s *alarmServer) writes() int { return int(s.mutes.Load() + s.resets.Load()) }

func newAlarmDevice(t *testing.T, s *alarmServer) *Device {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := sdk.ParsePort(portText)
	require.NoError(t, err)
	server := grpc.NewServer()
	mdk.RegisterSystemServiceServer(server, s)
	mdk.RegisterPowerServiceServer(server, s)
	mdk.RegisterTelemetryServiceServer(server, s)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	d, err := NewDevice(t.Context(), "ct1", sdk.DeviceInfo{Host: host, Port: 8080, RPCPort: port, URLScheme: "http"}, sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close(t.Context()) })
	return d
}

func supportedAlarmServer() *alarmServer {
	return &alarmServer{
		controls: &mdk.GetContainerControlsResponse{AlarmMuteSupported: new(true), AlarmResetSupported: new(true)},
		health:   &mdk.GetContainerHealthResponse{},
	}
}

func TestContainerAlarmActionSendsExactlyOneAcknowledgedCommand(t *testing.T) {
	for _, tc := range []struct {
		action        string
		mutes, resets int32
	}{
		{sdk.ContainerAlarmActionMute, 1, 0},
		{sdk.ContainerAlarmActionReset, 0, 1},
	} {
		t.Run(tc.action, func(t *testing.T) {
			s := supportedAlarmServer()
			d := newAlarmDevice(t, s)
			require.NoError(t, d.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: tc.action}))
			require.Equal(t, tc.mutes, s.mutes.Load())
			require.Equal(t, tc.resets, s.resets.Load())
		})
	}
}

func TestContainerAlarmActionIsAdvertised(t *testing.T) {
	var _ sdk.DeviceContainerAlarmController = &Device{}
	require.True(t, Capabilities()[sdk.CapabilityContainerAlarm])
}

func TestContainerAlarmActionRejectsInvalidAction(t *testing.T) {
	s := supportedAlarmServer()
	d := newAlarmDevice(t, s)
	err := d.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: "silence"})
	require.Error(t, err)
	require.Equal(t, sdk.WriteNotDispatched, sdk.WriteEvidence(err).GetReason())
	require.Zero(t, s.writes())
}

func TestContainerAlarmActionRefusesUnsupportedWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name, action string
		mutate       func(*alarmServer)
	}{
		{"mute flag absent", sdk.ContainerAlarmActionMute, func(s *alarmServer) { s.controls.AlarmMuteSupported = nil }},
		{"mute flag false", sdk.ContainerAlarmActionMute, func(s *alarmServer) { s.controls.AlarmMuteSupported = new(false) }},
		{"mute narrowed by live health", sdk.ContainerAlarmActionMute, func(s *alarmServer) { s.health.AlarmMuteSupported = new(false) }},
		{"reset flag absent", sdk.ContainerAlarmActionReset, func(s *alarmServer) { s.controls.AlarmResetSupported = nil }},
		{"reset flag false", sdk.ContainerAlarmActionReset, func(s *alarmServer) { s.controls.AlarmResetSupported = new(false) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := supportedAlarmServer()
			tc.mutate(s)
			d := newAlarmDevice(t, s)
			err := d.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: tc.action})
			require.Error(t, err)
			evidence := sdk.WriteEvidence(err)
			require.Equal(t, sdk.WriteNotDispatched, evidence.GetReason())
			require.Equal(t, string(sdk.WriteReasonUnsupported), evidence.GetMetadata()["reason"])
			require.Zero(t, s.writes())
		})
	}
}

func TestContainerAlarmMuteAllowedWhenHealthOmitsMuteSupport(t *testing.T) {
	s := supportedAlarmServer()
	s.health = &mdk.GetContainerHealthResponse{AlarmMuteSupported: nil}
	d := newAlarmDevice(t, s)
	require.NoError(t, d.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionMute}))
	require.EqualValues(t, 1, s.mutes.Load())
}

func TestContainerAlarmResetDoesNotDependOnMuteHealth(t *testing.T) {
	s := supportedAlarmServer()
	s.healthErr = status.Error(codes.Unavailable, "health down")
	d := newAlarmDevice(t, s)
	require.NoError(t, d.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionReset}))
	require.EqualValues(t, 1, s.resets.Load())
}

func TestContainerAlarmActionRefusesWhenPreflightReadFails(t *testing.T) {
	for _, tc := range []struct {
		name, action string
		mutate       func(*alarmServer)
	}{
		{"controls unreadable for mute", sdk.ContainerAlarmActionMute, func(s *alarmServer) { s.controlsErr = status.Error(codes.Unavailable, "down") }},
		{"controls unreadable for reset", sdk.ContainerAlarmActionReset, func(s *alarmServer) { s.controlsErr = status.Error(codes.Unavailable, "down") }},
		{"health unreadable for mute", sdk.ContainerAlarmActionMute, func(s *alarmServer) { s.healthErr = status.Error(codes.Unavailable, "down") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := supportedAlarmServer()
			tc.mutate(s)
			d := newAlarmDevice(t, s)
			err := d.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: tc.action})
			require.Error(t, err)
			evidence := sdk.WriteEvidence(err)
			require.Equal(t, sdk.WriteNotDispatched, evidence.GetReason())
			require.Equal(t, string(sdk.WriteReasonReadFailed), evidence.GetMetadata()["reason"])
			require.Zero(t, s.writes())
		})
	}
}

func TestContainerAlarmActionClassifiesControllerErrors(t *testing.T) {
	// A Frigate reason alone does not prove the pulse was not sent; only the
	// code does, until a reason is confirmed to precede any PLC write.
	notPrimary := (&sdk.FrigateRefusal{Reason: "HA_NOT_PRIMARY", Message: "controller is not the HA primary", Code: codes.Unavailable}).GRPCStatus().Err()
	unlistedReason := (&sdk.FrigateRefusal{Reason: "PLC_WRITE_FAILED", Message: "plc write failed", Code: codes.Internal}).GRPCStatus().Err()
	reasonedRefusal := (&sdk.FrigateRefusal{Reason: "CONTROL_IN_PROGRESS", Message: "busy", Code: codes.FailedPrecondition}).GRPCStatus().Err()
	for _, tc := range []struct {
		name    string
		err     error
		refused bool
	}{
		{"invalid argument", status.Error(codes.InvalidArgument, "bad"), true},
		{"control in progress", status.Error(codes.FailedPrecondition, "busy"), true},
		{"backend unimplemented", status.Error(codes.Unimplemented, "no"), true},
		{"refusal code with a reason", reasonedRefusal, true},
		{"ha not primary reason without a refusal code", notPrimary, false},
		{"unlisted reason with internal", unlistedReason, false},
		{"deadline exceeded", status.Error(codes.DeadlineExceeded, "slow"), false},
		{"internal", status.Error(codes.Internal, "plc"), false},
		{"unavailable", status.Error(codes.Unavailable, "gone"), false},
		{"unknown", status.Error(codes.Unknown, "?"), false},
	} {
		for _, action := range []string{sdk.ContainerAlarmActionMute, sdk.ContainerAlarmActionReset} {
			t.Run(tc.name+" "+action, func(t *testing.T) {
				s := supportedAlarmServer()
				s.writeErr = tc.err
				d := newAlarmDevice(t, s)
				err := d.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: action})
				require.Error(t, err)
				evidence := sdk.WriteEvidence(err)
				require.NotNil(t, evidence)
				if tc.refused {
					require.Equal(t, sdk.WriteNotDispatched, evidence.GetReason())
					require.Contains(t, err.Error(), "nothing was sent")
				} else {
					require.Equal(t, sdk.WriteUnknown, evidence.GetReason())
					require.Contains(t, err.Error(), "outcome unconfirmed")
				}
				require.Equal(t, 1, s.writes(), "a command must never be replayed")
			})
		}
	}
}

func TestContainerAlarmActionRefusesStandbyControllerWithoutWriting(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	for _, s := range []*haServer{a, b} {
		s.statusOverride = func(status *mdk.SystemStatus) {
			status.HaRole, status.ContainerWritePrimary = mdk.HARole_HA_ROLE_STANDBY, true
		}
	}
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	for _, action := range []string{sdk.ContainerAlarmActionMute, sdk.ContainerAlarmActionReset} {
		err = d.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: action})
		require.Error(t, err)
		evidence := sdk.WriteEvidence(err)
		require.Equal(t, sdk.WriteNotDispatched, evidence.GetReason())
		require.Equal(t, string(sdk.WriteStageBackendSelection), evidence.GetMetadata()["stage"])
	}
	require.Zero(t, a.writes.Load())
	require.Zero(t, b.writes.Load())
}
