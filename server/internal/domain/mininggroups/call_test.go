package mininggroups

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type callMinerGetter func(context.Context, int64) (interfaces.Miner, error)

func (get callMinerGetter) GetMinerForCommand(ctx context.Context, id int64) (interfaces.Miner, error) {
	return get(ctx, id)
}

type callMiningGroups struct {
	interfaces.Miner
	call func(context.Context, sdk.MiningGroupRequest) (sdk.MiningGroupResult, error)
}

func (m callMiningGroups) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	return m.call(ctx, request)
}

func TestCallClassifiesLocalRefusalsOnlyForMutations(t *testing.T) {
	for _, failure := range []string{"resolve handle", "unsupported adapter"} {
		for _, action := range []string{"deactivate", "observe"} {
			t.Run(failure+"/"+action, func(t *testing.T) {
				resolutionErr := errors.New("handle unavailable")
				s := &Service{miners: callMinerGetter(func(_ context.Context, id int64) (interfaces.Miner, error) {
					require.Equal(t, int64(7), id)
					if failure == "resolve handle" {
						return callMiningGroups{call: func(context.Context, sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
							t.Fatal("handle resolution failed; no adapter operation may run")
							return sdk.MiningGroupResult{}, nil
						}}, resolutionErr
					}
					return struct{ interfaces.Miner }{}, nil
				})}
				request := sdk.MiningGroupRequest{Action: action}
				if !request.ReadOnly() {
					request.Key, request.Revision = "saved-request", 2
				}
				result, err := s.call(t.Context(), 7, request)
				require.Error(t, err)
				require.Equal(t, sdk.MiningGroupResult{}, result)
				require.Equal(t, action == "deactivate", devicewrite.IsNotDispatched(err))
				if failure == "resolve handle" {
					require.ErrorIs(t, err, resolutionErr)
				} else {
					require.True(t, fleeterror.IsUnimplementedError(err), "classification must preserve the capability error")
				}
			})
		}
	}
}

func TestCallPreservesAdapterOutcomeEvidence(t *testing.T) {
	for _, tc := range []struct {
		name          string
		err           error
		notDispatched bool
		uncertain     bool
	}{
		{name: "success"},
		{name: "timeout", err: context.DeadlineExceeded},
		{name: "unimplemented", err: fleeterror.NewUnimplementedError("firmware rejected the request")},
		{name: "uncertain delivery", err: devicewrite.UncertainDelivery(errors.New("transport failed")), uncertain: true},
		{name: "local refusal", err: devicewrite.NotDispatched(errors.New("admission refused")), notDispatched: true},
		{name: "SDK refusal", err: sdk.UnsupportedWrite(sdk.CapabilityMiningGroups), notDispatched: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := sdk.MiningGroupRequest{Action: "deactivate", Key: "saved-request", Revision: 2}
			want := sdk.MiningGroupResult{Supported: true, Revision: 3}
			calls := 0
			m := callMiningGroups{call: func(_ context.Context, got sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
				calls++
				require.Equal(t, request, got)
				return want, tc.err
			}}
			s := &Service{miners: callMinerGetter(func(context.Context, int64) (interfaces.Miner, error) { return m, nil })}
			result, err := s.call(t.Context(), 7, request)
			require.Equal(t, 1, calls)
			require.Equal(t, want, result)
			require.Equal(t, tc.err, err, "the service must not reinterpret a returned adapter error")
			require.Equal(t, tc.notDispatched, devicewrite.IsNotDispatched(err))
			require.Equal(t, tc.uncertain, devicewrite.IsUncertainDelivery(err))
		})
	}
}
