package curtailment

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
)

// All v1 routes are wired and return CodeUnimplemented.
func TestHandler_AllRPCsReturnUnimplemented(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.Handle(curtailmentv1connect.NewCurtailmentServiceHandler(
		NewHandler(),
		connect.WithInterceptors(interceptors.NewErrorMappingInterceptor()),
	))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := curtailmentv1connect.NewCurtailmentServiceClient(http.DefaultClient, server.URL)

	cases := []struct {
		name string
		call func() error
	}{
		{
			"PreviewCurtailmentPlan",
			func() error {
				_, err := client.PreviewCurtailmentPlan(t.Context(), connect.NewRequest(&pb.PreviewCurtailmentPlanRequest{}))
				return err
			},
		},
		{
			"StartCurtailment",
			func() error {
				_, err := client.StartCurtailment(t.Context(), connect.NewRequest(&pb.StartCurtailmentRequest{}))
				return err
			},
		},
		{
			"UpdateCurtailmentEvent",
			func() error {
				_, err := client.UpdateCurtailmentEvent(t.Context(), connect.NewRequest(&pb.UpdateCurtailmentEventRequest{}))
				return err
			},
		},
		{
			"StopCurtailment",
			func() error {
				_, err := client.StopCurtailment(t.Context(), connect.NewRequest(&pb.StopCurtailmentRequest{}))
				return err
			},
		},
		{
			"GetActiveCurtailment",
			func() error {
				_, err := client.GetActiveCurtailment(t.Context(), connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))
				return err
			},
		},
		{
			"ListCurtailmentEvents",
			func() error {
				_, err := client.ListCurtailmentEvents(t.Context(), connect.NewRequest(&pb.ListCurtailmentEventsRequest{}))
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

func TestHandler_PriorityValidation(t *testing.T) {
	t.Parallel()

	client := newValidationTestClient(t)

	t.Run("Preview accepts EMERGENCY and reaches handler", func(t *testing.T) {
		t.Parallel()

		_, err := client.PreviewCurtailmentPlan(
			t.Context(),
			connect.NewRequest(validPreviewCurtailmentPlanRequest(pb.CurtailmentPriority_CURTAILMENT_PRIORITY_EMERGENCY)),
		)

		require.Error(t, err)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeUnimplemented, connectErr.Code())
	})

	t.Run("Preview rejects HIGH", func(t *testing.T) {
		t.Parallel()

		_, err := client.PreviewCurtailmentPlan(
			t.Context(),
			connect.NewRequest(validPreviewCurtailmentPlanRequest(pb.CurtailmentPriority_CURTAILMENT_PRIORITY_HIGH)),
		)

		require.Error(t, err)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	})

	t.Run("Start rejects HIGH", func(t *testing.T) {
		t.Parallel()

		_, err := client.StartCurtailment(
			t.Context(),
			connect.NewRequest(validStartCurtailmentRequest(pb.CurtailmentPriority_CURTAILMENT_PRIORITY_HIGH)),
		)

		require.Error(t, err)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	})
}

func newValidationTestClient(t *testing.T) curtailmentv1connect.CurtailmentServiceClient {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle(curtailmentv1connect.NewCurtailmentServiceHandler(
		NewHandler(),
		connect.WithInterceptors(
			interceptors.NewErrorMappingInterceptor(),
			validate.NewInterceptor(),
		),
	))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return curtailmentv1connect.NewCurtailmentServiceClient(http.DefaultClient, server.URL)
}

func validPreviewCurtailmentPlanRequest(priority pb.CurtailmentPriority) *pb.PreviewCurtailmentPlanRequest {
	return &pb.PreviewCurtailmentPlanRequest{
		Scope: &pb.PreviewCurtailmentPlanRequest_WholeOrg{
			WholeOrg: &pb.ScopeWholeOrg{},
		},
		Mode:     pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_PERCENT,
		Strategy: pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
		Level:    pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
		Priority: priority,
		ModeParams: &pb.PreviewCurtailmentPlanRequest_FixedPercent{
			FixedPercent: &pb.FixedPercentParams{Percent: 50},
		},
	}
}

func validStartCurtailmentRequest(priority pb.CurtailmentPriority) *pb.StartCurtailmentRequest {
	return &pb.StartCurtailmentRequest{
		Scope: &pb.StartCurtailmentRequest_WholeOrg{
			WholeOrg: &pb.ScopeWholeOrg{},
		},
		Mode:     pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_PERCENT,
		Strategy: pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
		Level:    pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
		Priority: priority,
		ModeParams: &pb.StartCurtailmentRequest_FixedPercent{
			FixedPercent: &pb.FixedPercentParams{Percent: 50},
		},
		Reason: "operator validation test",
	}
}
