package curtailment

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
)

// All v1 RPCs are stubbed; the contract for BE-1 is that every route is
// wired through Connect and returns CodeUnimplemented so later issues only
// have to swap in real business logic without touching main.go.
func TestHandler_AllRPCsReturnUnimplemented(t *testing.T) {
	t.Parallel()

	h := NewHandler()
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{
			"PreviewCurtailmentPlan",
			func() error {
				_, err := h.PreviewCurtailmentPlan(ctx, connect.NewRequest(&pb.PreviewCurtailmentPlanRequest{}))
				return err
			},
		},
		{
			"StartCurtailment",
			func() error {
				_, err := h.StartCurtailment(ctx, connect.NewRequest(&pb.StartCurtailmentRequest{}))
				return err
			},
		},
		{
			"UpdateCurtailmentEvent",
			func() error {
				_, err := h.UpdateCurtailmentEvent(ctx, connect.NewRequest(&pb.UpdateCurtailmentEventRequest{}))
				return err
			},
		},
		{
			"StopCurtailment",
			func() error {
				_, err := h.StopCurtailment(ctx, connect.NewRequest(&pb.StopCurtailmentRequest{}))
				return err
			},
		},
		{
			"GetActiveCurtailment",
			func() error {
				_, err := h.GetActiveCurtailment(ctx, connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))
				return err
			},
		},
		{
			"ListCurtailmentEvents",
			func() error {
				_, err := h.ListCurtailmentEvents(ctx, connect.NewRequest(&pb.ListCurtailmentEventsRequest{}))
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.call()
			require.Error(t, err)
			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr, "expected connect.Error, got %T", err)
			assert.Equal(t, connect.CodeUnimplemented, connectErr.Code())
		})
	}
}
