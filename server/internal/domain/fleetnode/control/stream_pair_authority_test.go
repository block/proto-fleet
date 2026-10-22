package control

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
)

func TestPairPersistMetaRevalidatesPhysicalAuthorityTuple(t *testing.T) {
	for _, field := range []string{"physical_node", "instance", "control_session", "epoch"} {
		t.Run(field, func(t *testing.T) {
			r, stream, route := haTestRegistry(t)
			session, err := r.Send(t.Context(), route.OwnerNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, haTestCommand(t, "pair"), nil, ReportKindPair, &PairMeta{OrgID: 1, Targets: map[string]struct{}{"miner": {}}})
			require.NoError(t, err)
			defer session.Close()
			<-stream.Outgoing
			_, meta, err := r.AdmitAndScopePairResults(route.PhysicalNodeID, "pair", []*pb.FleetNodePairResult{{DeviceIdentifier: "miner"}})
			require.NoError(t, err)
			require.Equal(t, route, meta.RouteBinding)
			durable := route
			r.SetHARouteResolver(func(context.Context, int64) (RouteBinding, error) {
				_, ok := r.HARoute(route.OwnerNodeID)
				require.True(t, ok, "resolver can inspect registry without deadlocking")
				return durable, nil
			})
			require.NoError(t, meta.ValidateAuthority(t.Context()))
			switch field {
			case "physical_node":
				durable.PhysicalNodeID++
			case "instance":
				durable.InstanceID = "replacement-instance"
			case "control_session":
				durable.ControlSessionID = "replacement-session"
			case "epoch":
				durable.Epoch++
			}
			require.Error(t, meta.ValidateAuthority(t.Context()), "in-process admission cannot override a changed durable tuple")
		})
	}
}

func TestPairPersistMetaLifetimeSurvivesOperatorCloseAndRenewalButCannotRevive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, stream, route := haTestRegistry(t)
		session, err := r.Send(t.Context(), route.OwnerNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, haTestCommand(t, "pair"), nil, ReportKindPair, &PairMeta{OrgID: 1, Targets: map[string]struct{}{"miner": {}}})
		require.NoError(t, err)
		<-stream.Outgoing
		_, meta, err := r.AdmitAndScopePairResults(route.PhysicalNodeID, "pair", []*pb.FleetNodePairResult{{DeviceIdentifier: "miner"}})
		require.NoError(t, err)
		ctx, cancel := meta.PersistenceContext(t.Context())
		defer cancel()
		session.Close()
		require.NoError(t, meta.ValidateAuthority(ctx), "operator lifetime must not discard an admitted pair result")
		time.Sleep(9 * time.Second)
		renewed := route
		renewed.ExpiresAt = time.Now().Add(10 * time.Second)
		r.SetHARoute(renewed)
		time.Sleep(2 * time.Second)
		require.NoError(t, ctx.Err(), "same-authority renewal must extend pending persistence")
		require.NoError(t, meta.ValidateAuthority(ctx))
		time.Sleep(8 * time.Second)
		synctest.Wait()
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		renewed.ExpiresAt = time.Now().Add(time.Minute)
		r.SetHARoute(renewed)
		require.Error(t, meta.ValidateAuthority(t.Context()), "a newly published lifetime cannot revive previously expired persistence")
	})
}
