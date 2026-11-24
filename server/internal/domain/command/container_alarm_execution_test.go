package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/authn"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAlarmActionWritableRequiresFreshAdvertisedAction(t *testing.T) {
	now := time.Now()
	fresh := timestamppb.New(now)
	for _, tc := range []struct {
		name   string
		state  *pb.ContainerControls
		action string
		want   bool
	}{
		{"missing controls", nil, sdk.ContainerAlarmActionMute, false},
		{"mute advertised", &pb.ContainerControls{ObservedAt: fresh, AlarmMuteSupported: new(true)}, sdk.ContainerAlarmActionMute, true},
		{"mute unadvertised", &pb.ContainerControls{ObservedAt: fresh}, sdk.ContainerAlarmActionMute, false},
		{"mute unsupported", &pb.ContainerControls{ObservedAt: fresh, AlarmMuteSupported: new(false)}, sdk.ContainerAlarmActionMute, false},
		{"reset advertised", &pb.ContainerControls{ObservedAt: fresh, AlarmResetSupported: new(true)}, sdk.ContainerAlarmActionReset, true},
		{"reset unsupported while mute supported", &pb.ContainerControls{ObservedAt: fresh, AlarmResetSupported: new(false), AlarmMuteSupported: new(true)}, sdk.ContainerAlarmActionReset, false},
		{"mute not implied by reset", &pb.ContainerControls{ObservedAt: fresh, AlarmResetSupported: new(true)}, sdk.ContainerAlarmActionMute, false},
		{"expired", &pb.ContainerControls{ObservedAt: timestamppb.New(now.Add(-3 * time.Minute)), AlarmMuteSupported: new(true)}, sdk.ContainerAlarmActionMute, false},
		{"future", &pb.ContainerControls{ObservedAt: timestamppb.New(now.Add(time.Minute)), AlarmMuteSupported: new(true)}, sdk.ContainerAlarmActionMute, false},
		{"stale flag", &pb.ContainerControls{ObservedAt: fresh, Stale: true, AlarmMuteSupported: new(true)}, sdk.ContainerAlarmActionMute, false},
		{"unknown action", &pb.ContainerControls{ObservedAt: fresh, AlarmMuteSupported: new(true), AlarmResetSupported: new(true)}, "silence", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, alarmActionWritable(tc.state, sdk.ContainerAlarmRequest{Action: tc.action}, now))
		})
	}
}

type alarmExecutionMiner struct {
	interfaces.Miner
	orgID      int64
	identifier string
	received   []sdk.ContainerAlarmRequest
	err        error
}

func (m *alarmExecutionMiner) GetOrgID() int64  { return m.orgID }
func (m *alarmExecutionMiner) GetSiteID() int64 { return 0 }
func (m *alarmExecutionMiner) GetID() models.DeviceIdentifier {
	return models.DeviceIdentifier(m.identifier)
}
func (m *alarmExecutionMiner) RunContainerAlarmAction(_ context.Context, req sdk.ContainerAlarmRequest) error {
	m.received = append(m.received, req)
	return m.err
}

func seedAlarmControls(t *testing.T, conn *sql.DB, deviceID int64, mute, reset *bool, observedAt time.Time) {
	t.Helper()
	_, err := conn.ExecContext(t.Context(), `DELETE FROM container_controls_observation WHERE device_id=$1`, deviceID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO container_controls_observation(device_id,observed_at,supported,alarm_mute_supported,alarm_reset_supported) VALUES($1,$2,true,$3,$4)`, deviceID, observedAt, mute, reset)
	require.NoError(t, err)
}

func queuedAlarmAction(t *testing.T, f *containerMiningSiteTest, action string) queue.Message {
	t.Helper()
	result, err := f.svc.RunContainerAlarmAction(authn.SetInfo(t.Context(), f.session), f.identifier, sdk.ContainerAlarmRequest{Action: action}, func(context.Context, *int64) error { return nil })
	require.NoError(t, err)
	messages, err := f.messageQueue.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, result.BatchIdentifier, messages[0].BatchLogUUID)
	require.Equal(t, commandtype.ContainerAlarmAction, messages[0].CommandType)
	return messages[0]
}

func TestContainerAlarmAdmissionRequiresCurrentAdvertisedSupport(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mute, reset  *bool
		observedAt   time.Duration
		action       string
		wantAdmitted bool
	}{
		{"mute supported", new(true), new(false), 0, sdk.ContainerAlarmActionMute, true},
		{"reset supported", new(false), new(true), 0, sdk.ContainerAlarmActionReset, true},
		{"reset unsupported on real controller shape", new(true), new(false), 0, sdk.ContainerAlarmActionReset, false},
		{"mute capability unknown", nil, new(true), 0, sdk.ContainerAlarmActionMute, false},
		{"controls expired", new(true), new(true), -3 * time.Minute, sdk.ContainerAlarmActionMute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupContainerMiningSiteTest(t)
			seedAlarmControls(t, f.conn, f.deviceID, tc.mute, tc.reset, time.Now().Add(tc.observedAt))
			_, err := f.svc.RunContainerAlarmAction(authn.SetInfo(t.Context(), f.session), f.identifier, sdk.ContainerAlarmRequest{Action: tc.action}, func(context.Context, *int64) error { return nil })
			var queued int
			require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM queue_message WHERE device_id=$1 AND command_type='ContainerAlarmAction'`, f.deviceID).Scan(&queued))
			if tc.wantAdmitted {
				require.NoError(t, err)
				require.Equal(t, 1, queued)
				return
			}
			require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
			require.Zero(t, queued)
		})
	}
}

func TestContainerAlarmAdmissionAuthorizesCurrentSiteAndRejectsInvalidActions(t *testing.T) {
	f := setupContainerMiningSiteTest(t)
	f.move(t, f.siteB)
	seedAlarmControls(t, f.conn, f.deviceID, new(true), new(true), time.Now())
	ctx := authn.SetInfo(t.Context(), f.session)
	var seen *int64
	_, err := f.svc.RunContainerAlarmAction(ctx, f.identifier, sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionMute}, func(_ context.Context, site *int64) error {
		seen = site
		return fleeterror.NewForbiddenError("denied")
	})
	require.True(t, fleeterror.IsForbiddenError(err))
	require.NotNil(t, seen)
	require.Equal(t, f.siteB, *seen, "authorization must use the container's current site")
	_, err = f.svc.RunContainerAlarmAction(ctx, f.identifier, sdk.ContainerAlarmRequest{Action: "silence"}, func(context.Context, *int64) error { return nil })
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	_, err = f.svc.RunContainerAlarmAction(ctx, f.identifier, sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionMute}, nil)
	require.True(t, fleeterror.IsForbiddenError(err))
	_, err = f.svc.RunContainerAlarmAction(ctx, "missing-controller", sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionMute}, func(context.Context, *int64) error { return nil })
	require.True(t, fleeterror.IsNotFoundError(err))
	var queued int
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM queue_message`).Scan(&queued))
	require.Zero(t, queued)
}

func TestContainerAlarmAndEquipmentControlExcludeEachOther(t *testing.T) {
	allow := func(context.Context, *int64) error { return nil }
	enable := sdk.ContainerControlRequest{Name: "dry_cooler", Enabled: new(true)}
	seed := func(t *testing.T, f *containerMiningSiteTest) {
		seedAlarmControls(t, f.conn, f.deviceID, new(true), new(true), time.Now())
		_, err := f.conn.ExecContext(t.Context(), `INSERT INTO container_control_observation(device_id,name,enabled) VALUES($1,'dry_cooler',false)`, f.deviceID)
		require.NoError(t, err)
	}
	t.Run("alarm then equipment control", func(t *testing.T) {
		f := setupContainerMiningSiteTest(t)
		seed(t, f)
		queuedAlarmAction(t, f, sdk.ContainerAlarmActionMute)
		_, err := f.svc.SetContainerControl(authn.SetInfo(t.Context(), f.session), f.identifier, enable, allow)
		require.True(t, fleeterror.IsFailedPreconditionError(err))
		require.ErrorContains(t, err, "already pending")
	})
	t.Run("equipment control then alarm", func(t *testing.T) {
		f := setupContainerMiningSiteTest(t)
		seed(t, f)
		_, err := f.svc.SetContainerControl(authn.SetInfo(t.Context(), f.session), f.identifier, enable, allow)
		require.NoError(t, err)
		_, err = f.svc.RunContainerAlarmAction(authn.SetInfo(t.Context(), f.session), f.identifier, sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionMute}, allow)
		require.True(t, fleeterror.IsFailedPreconditionError(err))
		require.ErrorContains(t, err, "already pending")
	})
	t.Run("alarm then alarm", func(t *testing.T) {
		f := setupContainerMiningSiteTest(t)
		seed(t, f)
		queuedAlarmAction(t, f, sdk.ContainerAlarmActionMute)
		_, err := f.svc.RunContainerAlarmAction(authn.SetInfo(t.Context(), f.session), f.identifier, sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionReset}, allow)
		require.ErrorContains(t, err, "already pending")
	})
}

func TestContainerAlarmExecutionSendsOnceAndClassifiesOutcomes(t *testing.T) {
	type outcome struct {
		name          string
		err           error
		wantErr       bool
		notDispatched bool
		contains      string
		notContains   string
	}
	unknown := sdk.WithWriteOutcome(errors.New("private transport detail"), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonTransportFailed)
	for _, tc := range []outcome{
		{name: "acknowledged"},
		{name: "refusal", err: devicewrite.NotDispatched(errors.New("controller refused: private reason")), wantErr: true, notDispatched: true, contains: "was not sent", notContains: "private"},
		{name: "unknown outcome", err: unknown, wantErr: true, contains: "outcome unconfirmed", notContains: "private"},
		{name: "deadline", err: context.DeadlineExceeded, wantErr: true, contains: "outcome unconfirmed"},
		{name: "unclassified failure", err: errors.New("PLC rejected"), wantErr: true, contains: "outcome unconfirmed"},
		{name: "old fleet node", err: devicewrite.NotDispatched(fmt.Errorf("private routing: %w", control.ErrCommandProtocolUpgradeRequired)), wantErr: true, notDispatched: true, contains: containerAlarmUpgradeMessage, notContains: "private"},
	} {
		for _, action := range []string{sdk.ContainerAlarmActionMute, sdk.ContainerAlarmActionReset} {
			t.Run(tc.name+" "+action, func(t *testing.T) {
				f := setupContainerMiningSiteTest(t)
				seedAlarmControls(t, f.conn, f.deviceID, new(true), new(true), time.Now())
				miner := &alarmExecutionMiner{Miner: f.miner, orgID: f.orgID, identifier: f.identifier, err: tc.err}
				f.execution.minerService = containerMiningSiteGetter{miner: miner}
				message := queuedAlarmAction(t, f, action)
				_, _, err := f.execution.executeQueuedCommand(t.Context(), message)
				require.Equal(t, []sdk.ContainerAlarmRequest{{Action: action}}, miner.received, "exactly one command, with the requested action")
				if !tc.wantErr {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
				require.True(t, fleeterror.IsFailedPreconditionError(err), "outcomes are terminal and never retried: %v", err)
				require.Equal(t, tc.notDispatched, devicewrite.IsNotDispatched(err))
				require.ErrorContains(t, err, tc.contains)
				if tc.notContains != "" {
					require.NotContains(t, err.Error(), tc.notContains)
				}
			})
		}
	}
}

func TestContainerAlarmExecutionRefusesExpiredCommandWithoutSending(t *testing.T) {
	for _, tc := range []struct {
		name       string
		admittedAt time.Time
	}{
		{"older than the bound", time.Now().Add(-containerAlarmMaxAge - time.Second)},
		{"no admission time", time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupContainerMiningSiteTest(t)
			seedAlarmControls(t, f.conn, f.deviceID, new(true), new(true), time.Now())
			miner := &alarmExecutionMiner{Miner: f.miner, orgID: f.orgID, identifier: f.identifier}
			f.execution.minerService = containerMiningSiteGetter{miner: miner}
			message := queuedAlarmAction(t, f, sdk.ContainerAlarmActionReset)
			var payload containerAlarmPayload
			require.NoError(t, json.Unmarshal(message.Payload, &payload))
			require.WithinDuration(t, time.Now(), payload.AdmittedAt, time.Minute, "admission records when the command was queued")
			payload.AdmittedAt = tc.admittedAt
			message.Payload, _ = json.Marshal(payload)
			_, _, err := f.execution.executeQueuedCommand(t.Context(), message)
			require.True(t, fleeterror.IsFailedPreconditionError(err), "an expired pulse is terminal: %v", err)
			require.True(t, devicewrite.IsNotDispatched(err))
			require.ErrorContains(t, err, "expired before it was sent")
			require.Empty(t, miner.received)
		})
	}
}

func TestContainerAlarmExecutionRejectsChangedPlacementWithoutSending(t *testing.T) {
	f := setupContainerMiningSiteTest(t)
	f.move(t, f.siteA)
	seedAlarmControls(t, f.conn, f.deviceID, new(true), new(true), time.Now())
	miner := &alarmExecutionMiner{Miner: f.miner, orgID: f.orgID, identifier: f.identifier}
	f.execution.minerService = containerMiningSiteGetter{miner: miner}
	message := queuedAlarmAction(t, f, sdk.ContainerAlarmActionMute)
	f.move(t, f.siteB)
	_, _, err := f.execution.executeQueuedCommand(t.Context(), message)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.ErrorContains(t, err, "ownership or site changed")
	require.Empty(t, miner.received)
}
