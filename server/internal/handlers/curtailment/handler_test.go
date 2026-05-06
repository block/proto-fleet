package curtailment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
)

// Non-admin-gated v1 routes are wired and return CodeUnimplemented when
// called without override fields. AdminTerminateEvent's Unimplemented body
// is covered by TestHandler_AdminTerminateEventRoleGate (admin/super-admin
// subcases), since its admin-role gate fires before the body.
func TestHandler_NonAdminRPCsReturnUnimplemented(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.Handle(curtailmentv1connect.NewCurtailmentServiceHandler(
		NewHandler(nil),
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

func TestHandler_RequestValidation(t *testing.T) {
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

	t.Run("Preview rejects maintenance inclusion without force", func(t *testing.T) {
		t.Parallel()

		req := validPreviewCurtailmentPlanRequest(pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL)
		req.IncludeMaintenance = true
		req.ForceIncludeMaintenance = false

		_, err := client.PreviewCurtailmentPlan(t.Context(), connect.NewRequest(req))

		require.Error(t, err)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	})

	t.Run("Preview rejects force without maintenance inclusion", func(t *testing.T) {
		t.Parallel()

		req := validPreviewCurtailmentPlanRequest(pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL)
		req.IncludeMaintenance = false
		req.ForceIncludeMaintenance = true

		_, err := client.PreviewCurtailmentPlan(t.Context(), connect.NewRequest(req))

		require.Error(t, err)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	})

	t.Run("Preview accepts maintenance inclusion with force and reaches handler", func(t *testing.T) {
		t.Parallel()

		req := validPreviewCurtailmentPlanRequest(pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL)
		req.IncludeMaintenance = true
		req.ForceIncludeMaintenance = true

		_, err := client.PreviewCurtailmentPlan(t.Context(), connect.NewRequest(req))

		require.Error(t, err)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeUnimplemented, connectErr.Code())
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

	t.Run("Start rejects maintenance inclusion without force", func(t *testing.T) {
		t.Parallel()

		req := validStartCurtailmentRequest(pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL)
		req.IncludeMaintenance = true
		req.ForceIncludeMaintenance = false

		_, err := client.StartCurtailment(t.Context(), connect.NewRequest(req))

		require.Error(t, err)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	})

	t.Run("Start rejects force without maintenance inclusion", func(t *testing.T) {
		t.Parallel()

		req := validStartCurtailmentRequest(pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL)
		req.IncludeMaintenance = false
		req.ForceIncludeMaintenance = true

		_, err := client.StartCurtailment(t.Context(), connect.NewRequest(req))

		require.Error(t, err)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	})

	t.Run("Start accepts maintenance inclusion with force and reaches handler", func(t *testing.T) {
		t.Parallel()

		req := validStartCurtailmentRequest(pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL)
		req.IncludeMaintenance = true
		req.ForceIncludeMaintenance = true

		_, err := client.StartCurtailment(t.Context(), connect.NewRequest(req))

		require.Error(t, err)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeUnimplemented, connectErr.Code())
	})
}

// AdminTerminateEvent rejects non-Admin roles before the Unimplemented body.
// Direct handler call so session.Info can be injected via authn.SetInfo.
func TestHandler_AdminTerminateEventRoleGate(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	req := connect.NewRequest(&pb.AdminTerminateEventRequest{
		EventUuid:   "event-uuid",
		TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
		Reason:      "operator role-gate test",
	})

	cases := []struct {
		name     string
		role     string
		wantCode connect.Code
	}{
		{"viewer role is rejected", "VIEWER", connect.CodePermissionDenied},
		{"empty role is rejected", "", connect.CodePermissionDenied},
		{"admin role reaches Unimplemented body", domainAuth.AdminRoleName, connect.CodeUnimplemented},
		{"super admin role reaches Unimplemented body", domainAuth.SuperAdminRoleName, connect.CodeUnimplemented},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := authn.SetInfo(t.Context(), &session.Info{
				Role: tc.role,
			})

			_, err := h.AdminTerminateEvent(ctx, req)

			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr, "expected fleeterror.FleetError, got %T", err)
			assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)
		})
	}
}

// buf.validate constraints on AdminTerminateEventRequest: event_uuid
// min_len, target_state restricted to CANCELLED/FAILED, reason min_len.
// Validator-passed requests reach the handler and surface CodeUnauthenticated
// from requireAdminFromContext (no session in context); we accept that as
// "validator passed". Role-gate behavior is covered by
// TestHandler_AdminTerminateEventRoleGate.
func TestHandler_AdminTerminateEventValidation(t *testing.T) {
	t.Parallel()

	client := newValidationTestClient(t)

	validReq := func() *pb.AdminTerminateEventRequest {
		return &pb.AdminTerminateEventRequest{
			EventUuid:   "00000000-0000-0000-0000-000000000001",
			TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
			Reason:      "operator validation test",
		}
	}

	cases := []struct {
		name     string
		mutate   func(*pb.AdminTerminateEventRequest)
		wantCode connect.Code
	}{
		{
			"valid CANCELLED reaches handler",
			func(*pb.AdminTerminateEventRequest) {},
			connect.CodeUnauthenticated,
		},
		{
			"valid FAILED reaches handler",
			func(r *pb.AdminTerminateEventRequest) {
				r.TargetState = pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_FAILED
			},
			connect.CodeUnauthenticated,
		},
		{
			"empty event_uuid is rejected",
			func(r *pb.AdminTerminateEventRequest) { r.EventUuid = "" },
			connect.CodeInvalidArgument,
		},
		{
			"empty reason is rejected",
			func(r *pb.AdminTerminateEventRequest) { r.Reason = "" },
			connect.CodeInvalidArgument,
		},
		{
			"target_state UNSPECIFIED is rejected",
			func(r *pb.AdminTerminateEventRequest) {
				r.TargetState = pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_UNSPECIFIED
			},
			connect.CodeInvalidArgument,
		},
		{
			"target_state PENDING is rejected",
			func(r *pb.AdminTerminateEventRequest) {
				r.TargetState = pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_PENDING
			},
			connect.CodeInvalidArgument,
		},
		{
			"target_state ACTIVE is rejected",
			func(r *pb.AdminTerminateEventRequest) {
				r.TargetState = pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE
			},
			connect.CodeInvalidArgument,
		},
		{
			"target_state RESTORING is rejected",
			func(r *pb.AdminTerminateEventRequest) {
				r.TargetState = pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_RESTORING
			},
			connect.CodeInvalidArgument,
		},
		{
			"target_state COMPLETED is rejected (would misreport real outcome)",
			func(r *pb.AdminTerminateEventRequest) {
				r.TargetState = pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_COMPLETED
			},
			connect.CodeInvalidArgument,
		},
		{
			"target_state COMPLETED_WITH_FAILURES is rejected (would misreport real outcome)",
			func(r *pb.AdminTerminateEventRequest) {
				r.TargetState = pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_COMPLETED_WITH_FAILURES
			},
			connect.CodeInvalidArgument,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := validReq()
			tc.mutate(req)

			_, err := client.AdminTerminateEvent(t.Context(), connect.NewRequest(req))

			require.Error(t, err)
			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			assert.Equal(t, tc.wantCode, connectErr.Code())
		})
	}
}

// Preview/Start/Stop reject non-Admin callers when an Admin-only override
// field is set, before any future body runs.
func TestHandler_OverrideFieldsRoleGate(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)

	type call struct {
		name       string
		invoke     func(ctx context.Context) error
		role       string
		authMethod session.AuthMethod
		wantCode   connect.Code
	}

	previewWithOverride := func(ctx context.Context) error {
		_, err := h.PreviewCurtailmentPlan(ctx, connect.NewRequest(&pb.PreviewCurtailmentPlanRequest{
			Scope: &pb.PreviewCurtailmentPlanRequest_WholeOrg{WholeOrg: &pb.ScopeWholeOrg{}},
			Mode:  pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.PreviewCurtailmentPlanRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 50},
			},
			CandidateMinPowerWOverride: ptr(uint32(800)),
		}))
		return err
	}
	startWithCandidateOverride := func(ctx context.Context) error {
		_, err := h.StartCurtailment(ctx, connect.NewRequest(&pb.StartCurtailmentRequest{
			Scope: &pb.StartCurtailmentRequest_WholeOrg{WholeOrg: &pb.ScopeWholeOrg{}},
			Mode:  pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.StartCurtailmentRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 50},
			},
			Reason:                     "override role-gate test",
			CandidateMinPowerWOverride: ptr(uint32(800)),
		}))
		return err
	}
	startWithAllowUnbounded := func(ctx context.Context) error {
		_, err := h.StartCurtailment(ctx, connect.NewRequest(&pb.StartCurtailmentRequest{
			Scope: &pb.StartCurtailmentRequest_WholeOrg{WholeOrg: &pb.ScopeWholeOrg{}},
			Mode:  pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.StartCurtailmentRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 50},
			},
			Reason:         "override role-gate test",
			AllowUnbounded: true,
		}))
		return err
	}
	stopWithBatchOverride := func(ctx context.Context) error {
		_, err := h.StopCurtailment(ctx, connect.NewRequest(&pb.StopCurtailmentRequest{
			EventUuid:                "00000000-0000-0000-0000-000000000001",
			RestoreBatchSizeOverride: ptr(uint32(50)),
		}))
		return err
	}

	cases := []call{
		// Non-admin role with override field set is rejected regardless of auth method.
		{"Preview override + viewer session", previewWithOverride, "VIEWER", session.AuthMethodSession, connect.CodePermissionDenied},
		{"Preview override + viewer API key", previewWithOverride, "VIEWER", session.AuthMethodAPIKey, connect.CodePermissionDenied},
		{"Start candidate override + viewer session", startWithCandidateOverride, "VIEWER", session.AuthMethodSession, connect.CodePermissionDenied},
		{"Start candidate override + viewer API key", startWithCandidateOverride, "VIEWER", session.AuthMethodAPIKey, connect.CodePermissionDenied},
		{"Start allow_unbounded + viewer session", startWithAllowUnbounded, "VIEWER", session.AuthMethodSession, connect.CodePermissionDenied},
		{"Start allow_unbounded + viewer API key", startWithAllowUnbounded, "VIEWER", session.AuthMethodAPIKey, connect.CodePermissionDenied},
		{"Stop batch override + viewer session", stopWithBatchOverride, "VIEWER", session.AuthMethodSession, connect.CodePermissionDenied},
		{"Stop batch override + viewer API key", stopWithBatchOverride, "VIEWER", session.AuthMethodAPIKey, connect.CodePermissionDenied},

		// Admin role reaches Unimplemented regardless of auth method — admin
		// API-key callers can drive override paths so external integrations
		// can use the override fields without an interactive session.
		{"Preview override + admin session", previewWithOverride, domainAuth.AdminRoleName, session.AuthMethodSession, connect.CodeUnimplemented},
		{"Preview override + admin API key", previewWithOverride, domainAuth.AdminRoleName, session.AuthMethodAPIKey, connect.CodeUnimplemented},
		{"Start candidate override + admin session", startWithCandidateOverride, domainAuth.AdminRoleName, session.AuthMethodSession, connect.CodeUnimplemented},
		{"Start candidate override + admin API key", startWithCandidateOverride, domainAuth.AdminRoleName, session.AuthMethodAPIKey, connect.CodeUnimplemented},
		{"Start allow_unbounded + super admin session", startWithAllowUnbounded, domainAuth.SuperAdminRoleName, session.AuthMethodSession, connect.CodeUnimplemented},
		{"Start allow_unbounded + super admin API key", startWithAllowUnbounded, domainAuth.SuperAdminRoleName, session.AuthMethodAPIKey, connect.CodeUnimplemented},
		{"Stop batch override + admin session", stopWithBatchOverride, domainAuth.AdminRoleName, session.AuthMethodSession, connect.CodeUnimplemented},
		{"Stop batch override + admin API key", stopWithBatchOverride, domainAuth.AdminRoleName, session.AuthMethodAPIKey, connect.CodeUnimplemented},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := authn.SetInfo(t.Context(), &session.Info{
				Role:       tc.role,
				AuthMethod: tc.authMethod,
			})

			err := tc.invoke(ctx)

			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr, "expected fleeterror.FleetError, got %T", err)
			assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)
		})
	}
}

// Without an override field, Preview/Start/Stop skip the role gate and
// reach Unimplemented — preserves API-key-accessible reads.
func TestHandler_NoOverrideSkipsRoleGate(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)

	previewNoOverride := connect.NewRequest(&pb.PreviewCurtailmentPlanRequest{
		Scope: &pb.PreviewCurtailmentPlanRequest_WholeOrg{WholeOrg: &pb.ScopeWholeOrg{}},
		Mode:  pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
		ModeParams: &pb.PreviewCurtailmentPlanRequest_FixedKw{
			FixedKw: &pb.FixedKwParams{TargetKw: 50},
		},
	})
	stopNoOverride := connect.NewRequest(&pb.StopCurtailmentRequest{
		EventUuid: "00000000-0000-0000-0000-000000000001",
	})

	// No session info in context — would fail role gate if invoked. The fact
	// that these reach Unimplemented proves the gate is skipped when no
	// override field is set.
	_, err := h.PreviewCurtailmentPlan(t.Context(), previewNoOverride)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnimplemented, fleetErr.GRPCCode, "Preview without override must skip role gate")

	_, err = h.StopCurtailment(t.Context(), stopNoOverride)
	require.Error(t, err)
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnimplemented, fleetErr.GRPCCode, "Stop without override must skip role gate")
}

// AdminTerminateEvent rejects a request with no session info in context.
func TestHandler_AdminTerminateEventRejectsMissingSession(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	req := connect.NewRequest(&pb.AdminTerminateEventRequest{
		EventUuid:   "event-uuid",
		TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
		Reason:      "missing-session test",
	})

	_, err := h.AdminTerminateEvent(t.Context(), req)

	require.Error(t, err)
	// Missing session.Info is remapped to CodeUnauthenticated; see handler.go.
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
}

func newValidationTestClient(t *testing.T) curtailmentv1connect.CurtailmentServiceClient {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle(curtailmentv1connect.NewCurtailmentServiceHandler(
		NewHandler(nil),
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
		Mode:     pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
		Strategy: pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
		Level:    pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
		Priority: priority,
		ModeParams: &pb.PreviewCurtailmentPlanRequest_FixedKw{
			FixedKw: &pb.FixedKwParams{TargetKw: 50},
		},
	}
}

func validStartCurtailmentRequest(priority pb.CurtailmentPriority) *pb.StartCurtailmentRequest {
	return &pb.StartCurtailmentRequest{
		Scope: &pb.StartCurtailmentRequest_WholeOrg{
			WholeOrg: &pb.ScopeWholeOrg{},
		},
		Mode:     pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
		Strategy: pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
		Level:    pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
		Priority: priority,
		ModeParams: &pb.StartCurtailmentRequest_FixedKw{
			FixedKw: &pb.FixedKwParams{TargetKw: 50},
		},
		Reason: "operator validation test",
	}
}

// ptr returns a pointer to v, for setting proto3 optional fields in tests.
func ptr[T any](v T) *T { return &v }
