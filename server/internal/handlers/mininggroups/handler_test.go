package mininggroups

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/mininggroups/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/stretchr/testify/require"
)

type rejectingVerifier struct{ username, password string }

func (v *rejectingVerifier) VerifySessionCredentials(_ context.Context, username, password string) error {
	v.username, v.password = username, password
	return errors.New("step-up rejected")
}

func TestSetPoolsRequiresSessionCredentialsBeforeAdmission(t *testing.T) {
	verifier := &rejectingVerifier{}
	handler := NewHandler(nil, nil, verifier)
	ctx := authn.SetInfo(t.Context(), &session.Info{AuthMethod: session.AuthMethodSession})
	_, err := handler.SetPools(ctx, connect.NewRequest(&pb.SetPoolsRequest{UserUsername: "operator", UserPassword: "incorrect"}))
	require.EqualError(t, err, "step-up rejected")
	require.Equal(t, "operator", verifier.username)
	require.Equal(t, "incorrect", verifier.password)
}

func TestMiningGroupRPCsRequireOperatorSession(t *testing.T) {
	for _, principal := range []struct {
		name string
		info session.Info
	}{
		{name: "API key", info: session.Info{AuthMethod: session.AuthMethodAPIKey}},
		{name: "account key", info: session.Info{AuthMethod: session.AuthMethodAPIKey, AccountKey: true}},
	} {
		t.Run(principal.name, func(t *testing.T) {
			ctx := authn.SetInfo(t.Context(), &principal.info)
			handler := NewHandler(nil, nil, &rejectingVerifier{})
			for _, rpc := range []struct {
				name string
				call func() error
			}{
				{"GetState", func() error { _, err := handler.GetState(ctx, connect.NewRequest(&pb.GetStateRequest{})); return err }},
				{"Activate", func() error {
					_, err := handler.Activate(ctx, connect.NewRequest(&pb.ActivateRequest{AcknowledgeInterruption: true}))
					return err
				}},
				{"SetPools", func() error { _, err := handler.SetPools(ctx, connect.NewRequest(&pb.SetPoolsRequest{})); return err }},
				{"ResolveFleetPools", func() error {
					_, err := handler.ResolveFleetPools(ctx, connect.NewRequest(&pb.ResolveFleetPoolsRequest{}))
					return err
				}},
				{"ListPoolConfigurations", func() error {
					_, err := handler.ListPoolConfigurations(ctx, connect.NewRequest(&pb.ListPoolConfigurationsRequest{}))
					return err
				}},
				{"CreatePoolConfiguration", func() error {
					_, err := handler.CreatePoolConfiguration(ctx, connect.NewRequest(&pb.CreatePoolConfigurationRequest{}))
					return err
				}},
			} {
				t.Run(rpc.name, func(t *testing.T) {
					err := rpc.call()
					var denied fleeterror.FleetError
					require.ErrorAs(t, err, &denied)
					require.Equal(t, connect.CodePermissionDenied, denied.GRPCCode)
					require.Equal(t, "operator access required", denied.DebugMessage)
				})
			}
		})
	}
}
