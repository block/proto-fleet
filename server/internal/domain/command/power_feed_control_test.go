package command

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/authn"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

const controlFeed = "pdu-2-outlet-05"

func allowPowerFeed(context.Context, *int64) error { return nil }

func (f *containerMiningSiteTest) queuePowerFeed(t *testing.T, module int64, action string) queue.Message {
	t.Helper()
	sites := []int64{}
	result, identifier, err := f.svc.ControlModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, f.identifier, action, controlFeed, func(_ context.Context, site *int64) error {
		require.NotNil(t, site)
		sites = append(sites, *site)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, f.identifier, identifier)
	require.Equal(t, []int64{f.siteA}, sites, "authorized once, at the module's current site")
	messages, err := f.messageQueue.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, result.BatchIdentifier, messages[0].BatchLogUUID)
	return messages[0]
}

func TestControlModulePowerFeedQueuesTheConfirmedFeedOnce(t *testing.T) {
	for _, action := range []string{sdk.ContainerPowerFeedTurnOn, sdk.ContainerPowerFeedTurnOff, sdk.ContainerPowerFeedCycle, sdk.ContainerPowerFeedClearOverride} {
		t.Run(action, func(t *testing.T) {
			f, miner, module := setupPowerFeedTest(t, true)
			message := f.queuePowerFeed(t, module, action)
			require.Equal(t, commandtype.ControlPowerFeed, message.CommandType)
			var payload modulePowerFeedPayload
			require.NoError(t, json.Unmarshal(message.Payload, &payload))
			want := sdk.ContainerPowerFeedRequest{Action: action, FeedID: controlFeed, Module: sdk.ContainerPowerFeedModule{Tank: 2, Module: 5, ReportedID: "module-x"}}
			require.WithinDuration(t, time.Now(), payload.AdmittedAt, time.Minute, "admission records when the command was queued")
			require.Equal(t, modulePowerFeedPayload{Request: want, OrgID: f.orgID, SiteID: f.siteA, ModuleID: module, AdmittedAt: payload.AdmittedAt}, payload)

			_, _, err := f.svc.ControlModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, f.identifier, action, controlFeed, allowPowerFeed)
			require.True(t, fleeterror.IsFailedPreconditionError(err), "a second command waits for the first: %v", err)

			_, _, err = f.execution.executeQueuedCommand(t.Context(), message)
			require.NoError(t, err)
			require.Equal(t, []sdk.ContainerPowerFeedRequest{want}, miner.requests, "written exactly once")

			// The dialog follows the write through the equipment result lookup.
			result, err := f.svc.GetContainerControlResult(authn.SetInfo(t.Context(), f.session), f.identifier, message.BatchLogUUID, allowPowerFeed)
			require.NoError(t, err, "the container owns its power feed batch")
			require.EqualValues(t, 1, result.TotalCount)
		})
	}
}

func TestControlModulePowerFeedRefusesBeforeQueueing(t *testing.T) {
	denied := errors.New("denied")
	for _, tc := range []struct {
		name      string
		confirmed bool
		setup     func(*testing.T, *containerMiningSiteTest)
		action    string
		auth      modulePowerFeedAuthorization
		check     func(error) bool
	}{
		{name: "denied", confirmed: true, action: sdk.ContainerPowerFeedTurnOff, auth: func(context.Context, *int64) error { return denied }, check: func(err error) bool { return errors.Is(err, denied) }},
		{name: "unconfirmed identity", action: sdk.ContainerPowerFeedTurnOff, auth: allowPowerFeed, check: fleeterror.IsFailedPreconditionError},
		{name: "no control capability", confirmed: true, action: sdk.ContainerPowerFeedTurnOff, auth: allowPowerFeed, setup: func(_ *testing.T, f *containerMiningSiteTest) {
			f.svc.SetPluginCapabilitiesProvider(powerFeedCapabilities{supported: true})
		}, check: fleeterror.IsFailedPreconditionError},
		{name: "fleet node below V16", confirmed: true, action: sdk.ContainerPowerFeedTurnOff, auth: allowPowerFeed, setup: func(t *testing.T, f *containerMiningSiteTest) {
			routeThroughFleetNode(t, f, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V15)
		}, check: func(err error) bool {
			return fleeterror.IsFailedPreconditionError(err) && strings.Contains(err.Error(), powerFeedControlUpgradeMessage)
		}},
		{name: "inspect is not a write", confirmed: true, action: sdk.ContainerPowerFeedInspect, auth: allowPowerFeed, check: fleeterror.IsInvalidArgumentError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, miner, module := setupPowerFeedTest(t, tc.confirmed)
			if tc.setup != nil {
				tc.setup(t, f)
			}
			_, _, err := f.svc.ControlModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, f.identifier, tc.action, controlFeed, tc.auth)
			require.True(t, tc.check(err), err)
			messages, err := f.messageQueue.Dequeue(t.Context(), 1)
			require.NoError(t, err)
			require.Empty(t, messages)
			require.Empty(t, miner.requests)
		})
	}
	// Feed IDs repeat across containers: a module re-homed after the operator
	// inspected it must not be written on its new controller.
	t.Run("module now belongs to another controller", func(t *testing.T) {
		f, miner, module := setupPowerFeedTest(t, true)
		authorized := false
		_, _, err := f.svc.ControlModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, "another-controller", sdk.ContainerPowerFeedTurnOff, controlFeed, func(context.Context, *int64) error {
			authorized = true
			return nil
		})
		require.True(t, fleeterror.IsFailedPreconditionError(err), err)
		require.ErrorContains(t, err, powerFeedControlChangedReason)
		require.False(t, authorized)
		messages, err := f.messageQueue.Dequeue(t.Context(), 1)
		require.NoError(t, err)
		require.Empty(t, messages)
		require.Empty(t, miner.requests)
	})
	t.Run("unknown module", func(t *testing.T) {
		f, _, module := setupPowerFeedTest(t, true)
		_, _, err := f.svc.ControlModulePowerFeed(authn.SetInfo(t.Context(), f.session), module+100, f.identifier, sdk.ContainerPowerFeedTurnOff, controlFeed, allowPowerFeed)
		require.True(t, fleeterror.IsNotFoundError(err), err)
	})
}

func TestControlModulePowerFeedExecutionNeverRetriesOrReplays(t *testing.T) {
	for _, tc := range []struct {
		name     string
		writeErr error
		contains string
	}{
		{name: "controller refusal", writeErr: devicewrite.NotDispatched(fleeterror.NewFailedPreconditionError("container power feed: tank 2 module 5 is now supplied by pdu-9, not pdu-2-outlet-05")), contains: "now supplied by"},
		{name: "unsent for another reason", writeErr: devicewrite.NotDispatched(errors.New("private transport detail")), contains: "was not sent"},
		{name: "unknown outcome", writeErr: errors.New("private transport detail"), contains: "outcome unconfirmed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, miner, module := setupPowerFeedTest(t, true)
			miner.writeErr = tc.writeErr
			message := f.queuePowerFeed(t, module, sdk.ContainerPowerFeedTurnOff)
			_, _, err := f.execution.executeQueuedCommand(t.Context(), message)
			require.True(t, fleeterror.IsFailedPreconditionError(err), "failed preconditions are terminal in the queue: %v", err)
			require.ErrorContains(t, err, tc.contains)
			require.NotContains(t, err.Error(), "private")
			require.Len(t, miner.requests, 1)
		})
	}
}

func TestControlModulePowerFeedExecutionRefusesExpiredCommandWithoutSending(t *testing.T) {
	for _, tc := range []struct {
		name       string
		admittedAt time.Time
	}{
		{"older than the bound", time.Now().Add(-powerFeedControlMaxAge - time.Second)},
		{"no admission time", time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, miner, module := setupPowerFeedTest(t, true)
			message := f.queuePowerFeed(t, module, sdk.ContainerPowerFeedTurnOff)
			var payload modulePowerFeedPayload
			require.NoError(t, json.Unmarshal(message.Payload, &payload))
			payload.AdmittedAt = tc.admittedAt
			var err error
			message.Payload, err = json.Marshal(payload)
			require.NoError(t, err)
			_, _, err = f.execution.executeQueuedCommand(t.Context(), message)
			require.True(t, fleeterror.IsFailedPreconditionError(err), "an expired write is terminal: %v", err)
			require.True(t, devicewrite.IsNotDispatched(err))
			require.ErrorContains(t, err, "expired before it was sent")
			require.Empty(t, miner.requests)
		})
	}
}

type failingPowerFeedMinerGetter struct{ CachedMinerGetter }

func (failingPowerFeedMinerGetter) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return nil, errors.New("private connection detail")
}

func TestControlModulePowerFeedExecutionMakesFailuresBeforeDispatchTerminal(t *testing.T) {
	t.Run("preparation fails", func(t *testing.T) {
		f, miner, module := setupPowerFeedTest(t, true)
		message := f.queuePowerFeed(t, module, sdk.ContainerPowerFeedTurnOff)
		var payload modulePowerFeedPayload
		require.NoError(t, json.Unmarshal(message.Payload, &payload))
		ctx, cancel := context.WithCancel(t.Context())
		cancel() // The placement transaction fails before anything is sent.
		err := f.execution.executeModulePowerFeed(ctx, f.deviceID, f.orgID, payload, miner)
		require.True(t, fleeterror.IsFailedPreconditionError(err), "the queue must not retry it: %v", err)
		require.True(t, devicewrite.IsNotDispatched(err))
		require.ErrorContains(t, err, "was not sent")
		require.Empty(t, miner.requests)
	})
	t.Run("controller lookup fails", func(t *testing.T) {
		f, miner, module := setupPowerFeedTest(t, true)
		message := f.queuePowerFeed(t, module, sdk.ContainerPowerFeedTurnOff)
		f.execution.minerService = failingPowerFeedMinerGetter{}
		_, _, err := f.execution.executeQueuedCommand(t.Context(), message)
		require.True(t, fleeterror.IsFailedPreconditionError(err), "the queue must not retry it: %v", err)
		require.ErrorContains(t, err, "was not sent")
		require.NotContains(t, err.Error(), "private")
		require.Empty(t, miner.requests)
	})
}

func TestControlModulePowerFeedExecutionRequiresTheConfirmedModule(t *testing.T) {
	for name, change := range map[string]string{
		"replaced":    `UPDATE container_module SET reported_id='module-y' WHERE id=$1`,
		"unconfirmed": `UPDATE container_module SET identity_confirmed=false WHERE id=$1`,
	} {
		t.Run(name, func(t *testing.T) {
			f, miner, module := setupPowerFeedTest(t, true)
			message := f.queuePowerFeed(t, module, sdk.ContainerPowerFeedTurnOff)
			_, err := f.conn.ExecContext(t.Context(), change, module)
			require.NoError(t, err)
			_, _, err = f.execution.executeQueuedCommand(t.Context(), message)
			require.True(t, fleeterror.IsFailedPreconditionError(err), err)
			require.Empty(t, miner.requests)
		})
	}
	t.Run("moved site", func(t *testing.T) {
		f, miner, module := setupPowerFeedTest(t, true)
		message := f.queuePowerFeed(t, module, sdk.ContainerPowerFeedTurnOff)
		f.move(t, f.siteB)
		_, _, err := f.execution.executeQueuedCommand(t.Context(), message)
		require.True(t, fleeterror.IsFailedPreconditionError(err), err)
		require.Empty(t, miner.requests)
	})
}

func TestReadModulePowerFeedOffersControlsOnlyWhenSafe(t *testing.T) {
	on := func() *sdk.PowerFeedReading {
		return &sdk.PowerFeedReading{ID: controlFeed, State: sdk.PowerFeedStateEnabled, Faulted: new(false)}
	}
	inspection := func(change func(*sdk.ModulePowerFeedInspection)) *sdk.ModulePowerFeedInspection {
		i := &sdk.ModulePowerFeedInspection{
			ObservedAt: time.Now(), RecoveryCycleKnown: true, Feed: on(),
			Mapping: sdk.PowerFeedMapping{Confidence: sdk.PowerFeedMappingWiringVerified, FeedID: controlFeed},
		}
		if change != nil {
			change(i)
		}
		return i
	}
	for _, tc := range []struct {
		name       string
		inspection *sdk.ModulePowerFeedInspection
		pending    bool
		actions    []string
		reason     string
	}{
		{name: "on", inspection: inspection(nil), actions: []string{sdk.ContainerPowerFeedTurnOff, sdk.ContainerPowerFeedCycle}},
		{name: "off and overridden", inspection: inspection(func(i *sdk.ModulePowerFeedInspection) {
			i.Feed.State, i.Feed.Overridden = sdk.PowerFeedStateDisabled, true
		}), actions: []string{sdk.ContainerPowerFeedTurnOn, sdk.ContainerPowerFeedClearOverride}},
		{name: "fault unknown cannot cycle", inspection: inspection(func(i *sdk.ModulePowerFeedInspection) { i.Feed.Faulted = nil }), actions: []string{sdk.ContainerPowerFeedTurnOff}},
		{name: "ambiguous mapping", inspection: inspection(func(i *sdk.ModulePowerFeedInspection) {
			i.Mapping, i.Feed = sdk.PowerFeedMapping{Confidence: sdk.PowerFeedMappingAmbiguous, Reasons: []string{"feed_shared"}}, nil
		}), reason: powerFeedControlMappingReason},
		{name: "no reading", inspection: inspection(func(i *sdk.ModulePowerFeedInspection) {
			i.Feed, i.FeedUnavailableReason = nil, "read_failed"
		}), reason: powerFeedControlNoReadingReason},
		{name: "recovery state unknown", inspection: inspection(func(i *sdk.ModulePowerFeedInspection) { i.RecoveryCycleKnown = false }), actions: []string{sdk.ContainerPowerFeedTurnOff, sdk.ContainerPowerFeedCycle}, reason: powerFeedControlCycleUnknown},
		{name: "recovery cycle active", inspection: inspection(func(i *sdk.ModulePowerFeedInspection) {
			i.RecoveryCycle = &sdk.PowerRecoveryCycle{ID: "cycle-1", Phase: "holding", OutletIDs: []string{controlFeed}}
		}), actions: []string{sdk.ContainerPowerFeedTurnOff, sdk.ContainerPowerFeedCycle}, reason: powerFeedControlCycleActive},
		{name: "state unknown", inspection: inspection(func(i *sdk.ModulePowerFeedInspection) { i.Feed.State = sdk.PowerFeedStateUnknown }), reason: powerFeedControlStateReason},
		{name: "command pending", inspection: inspection(nil), pending: true, actions: []string{sdk.ContainerPowerFeedTurnOff, sdk.ContainerPowerFeedCycle}, reason: powerFeedControlPendingReason},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, miner, module := setupPowerFeedTest(t, true)
			if tc.pending {
				_, _, err := f.svc.ControlModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, f.identifier, sdk.ContainerPowerFeedTurnOff, controlFeed, allowPowerFeed)
				require.NoError(t, err)
			}
			miner.inspection = tc.inspection
			got, err := f.svc.ReadModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, allowPowerFeed, func(_ context.Context, site *int64) bool {
				require.NotNil(t, site)
				require.Equal(t, f.siteA, *site)
				return true
			})
			require.NoError(t, err)
			require.NotNil(t, got.Control)
			require.Equal(t, tc.actions, got.Control.Actions)
			require.Equal(t, tc.reason, got.Control.UnavailableReason)
		})
	}
	t.Run("without permission", func(t *testing.T) {
		f, miner, module := setupPowerFeedTest(t, true)
		miner.inspection = inspection(nil)
		got, err := f.svc.ReadModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, allowPowerFeed, func(context.Context, *int64) bool { return false })
		require.NoError(t, err)
		require.Nil(t, got.Control)
	})
	t.Run("controller cannot control feeds", func(t *testing.T) {
		f, miner, module := setupPowerFeedTest(t, true)
		miner.inspection = inspection(nil)
		f.svc.SetPluginCapabilitiesProvider(powerFeedCapabilities{supported: true})
		got, err := f.svc.ReadModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, allowPowerFeed, func(context.Context, *int64) bool { return true })
		require.NoError(t, err)
		require.Equal(t, powerFeedControlUnsupportedReason, got.Control.UnavailableReason)
	})
	t.Run("fleet node inspects but cannot write", func(t *testing.T) {
		f, miner, module := setupPowerFeedTest(t, true)
		miner.inspection = inspection(nil)
		routeThroughFleetNode(t, f, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V15)
		got, err := f.svc.ReadModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, allowPowerFeed, func(context.Context, *int64) bool { return true })
		require.NoError(t, err)
		require.Equal(t, powerFeedControlUpgradeReason, got.Control.UnavailableReason)
	})
	t.Run("unconfirmed identity", func(t *testing.T) {
		f, miner, module := setupPowerFeedTest(t, false)
		miner.inspection = inspection(nil)
		got, err := f.svc.ReadModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, allowPowerFeed, func(context.Context, *int64) bool { return true })
		require.NoError(t, err)
		require.Equal(t, powerFeedControlIdentityReason, got.Control.UnavailableReason)
	})
}
