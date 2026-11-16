package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/authn"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type powerFeedCapabilities struct{ supported bool }

func (p powerFeedCapabilities) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityContainerPowerFeed: p.supported}
}

type powerFeedMiner struct {
	*containerMiningSiteMiner
	requests []sdk.ContainerPowerFeedRequest
}

func (m *powerFeedMiner) ContainerPowerFeed(_ context.Context, req sdk.ContainerPowerFeedRequest) (sdk.ContainerPowerFeedResult, error) {
	m.requests = append(m.requests, req)
	return sdk.ContainerPowerFeedResult{Inspection: &sdk.ModulePowerFeedInspection{
		ObservedAt: time.Now(), Mapping: sdk.PowerFeedMapping{Confidence: sdk.PowerFeedMappingConfigured, FeedID: "pdu-2-outlet-05"}, RecoveryCycleKnown: true,
	}}, nil
}

func setupPowerFeedTest(t *testing.T, confirmed bool) (*containerMiningSiteTest, *powerFeedMiner, int64) {
	t.Helper()
	f := setupContainerMiningSiteTest(t)
	f.move(t, f.siteA)
	ctx := t.Context()
	_, err := f.conn.ExecContext(ctx, `INSERT INTO container_inventory(device_id,observed_at,org_id) VALUES($1,now(),$2)`, f.deviceID, f.orgID)
	require.NoError(t, err)
	var tank, module int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,2,now(),now()) RETURNING id`, f.deviceID).Scan(&tank))
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,identity_confirmed,first_seen_at,last_seen_at,state) VALUES($1,5,'module-x',$2,now(),now(),'active') RETURNING id`, tank, confirmed).Scan(&module))
	miner := &powerFeedMiner{containerMiningSiteMiner: f.miner}
	f.execution.minerService = containerMiningSiteGetter{miner: miner}
	f.svc.SetPluginCapabilitiesProvider(powerFeedCapabilities{supported: true})
	return f, miner, module
}

func TestReadModulePowerFeedAuthorizesAtSiteAndSendsConfirmedPlacement(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		f, miner, module := setupPowerFeedTest(t, confirmed)
		sites := []int64{}
		got, err := f.svc.ReadModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, func(_ context.Context, site *int64) error {
			require.NotNil(t, site)
			sites = append(sites, *site)
			return nil
		})
		require.NoError(t, err)
		require.NotEmpty(t, sites)
		for _, site := range sites {
			require.Equal(t, f.siteA, site)
		}
		want := sdk.ContainerPowerFeedModule{Tank: 2, Module: 5}
		if confirmed {
			want.ReportedID = "module-x"
		}
		require.Equal(t, []sdk.ContainerPowerFeedRequest{{Action: sdk.ContainerPowerFeedInspect, Module: want}}, miner.requests, "an unconfirmed identity is never asserted")
		require.Equal(t, module, got.ModuleID)
		require.Equal(t, f.identifier, got.DeviceIdentifier)
		require.EqualValues(t, 2, got.Tank)
		require.EqualValues(t, 5, got.Module)
		require.Equal(t, "pdu-2-outlet-05", got.Inspection.Mapping.FeedID)
	}
}

func TestReadModulePowerFeedRefusesBeforeControllerIO(t *testing.T) {
	allow := func(context.Context, *int64) error { return nil }
	denied := errors.New("denied")
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, *containerMiningSiteTest)
		auth  modulePowerFeedAuthorization
		check func(error) bool
	}{
		{name: "denied", auth: func(context.Context, *int64) error { return denied }, check: func(err error) bool { return errors.Is(err, denied) }},
		{name: "unsupported", setup: func(_ *testing.T, f *containerMiningSiteTest) {
			f.svc.SetPluginCapabilitiesProvider(powerFeedCapabilities{})
		}, auth: allow, check: fleeterror.IsFailedPreconditionError},
		{name: "fleet node below V13", setup: func(t *testing.T, f *containerMiningSiteTest) {
			var node int64
			require.NoError(t, f.conn.QueryRowContext(t.Context(), `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES($1,'feed-node',$2,$3,'CONFIRMED') RETURNING id`, f.orgID, []byte("feed-identity"), []byte("01234567890123456789012345678901")).Scan(&node))
			_, err := f.conn.ExecContext(t.Context(), `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,$3)`, node, f.deviceID, f.orgID)
			require.NoError(t, err)
			registry := control.NewRegistry()
			stream, err := registry.RegisterAuthenticated(node, "session", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V11)
			require.NoError(t, err)
			t.Cleanup(stream.Unregister)
			f.svc.SetCommandProtocolChecker(registry)
		}, auth: allow, check: fleeterror.IsFailedPreconditionError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, miner, module := setupPowerFeedTest(t, true)
			if tc.setup != nil {
				tc.setup(t, f)
			}
			_, err := f.svc.ReadModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, tc.auth)
			require.True(t, tc.check(err), err)
			require.Empty(t, miner.requests)
		})
	}
	t.Run("unknown module", func(t *testing.T) {
		f, miner, module := setupPowerFeedTest(t, true)
		_, err := f.svc.ReadModulePowerFeed(authn.SetInfo(t.Context(), f.session), module+100, allow)
		require.True(t, fleeterror.IsNotFoundError(err), err)
		require.Empty(t, miner.requests)
	})
}

// The site is authorized again under the binding lock, so a container moved
// to a site the caller cannot read after the first check is never queried.
func TestReadModulePowerFeedReauthorizesAfterSiteChange(t *testing.T) {
	f, miner, module := setupPowerFeedTest(t, true)
	denied := errors.New("denied")
	sites := []int64{}
	_, err := f.svc.ReadModulePowerFeed(authn.SetInfo(t.Context(), f.session), module, func(_ context.Context, site *int64) error {
		require.NotNil(t, site)
		sites = append(sites, *site)
		if len(sites) == 1 {
			f.move(t, f.siteB)
			return nil
		}
		return denied
	})
	require.ErrorIs(t, err, denied)
	require.Equal(t, []int64{f.siteA, f.siteB}, sites)
	require.Empty(t, miner.requests, "no request reaches the controller")
}
