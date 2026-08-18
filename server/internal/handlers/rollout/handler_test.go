package rollout

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	rolloutDomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func TestHandlerGatesEveryRolloutRPC(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil)
	ctx := rolloutHandlerContext(t, 42, 9)
	rolloutID := uuid.New().String()
	calls := []struct {
		name string
		call func() error
	}{
		{"CreateRolloutLane", func() error {
			_, err := handler.CreateRolloutLane(ctx, connect.NewRequest(&pb.CreateRolloutLaneRequest{}))
			return err
		}},
		{"GetRolloutLane", func() error {
			_, err := handler.GetRolloutLane(ctx, connect.NewRequest(&pb.GetRolloutLaneRequest{LaneId: rolloutID}))
			return err
		}},
		{"ListRolloutLanes", func() error {
			_, err := handler.ListRolloutLanes(ctx, connect.NewRequest(&pb.ListRolloutLanesRequest{}))
			return err
		}},
		{"StartRolloutLane", func() error {
			_, err := handler.StartRolloutLane(ctx, connect.NewRequest(&pb.StartRolloutLaneRequest{LaneId: rolloutID}))
			return err
		}},
		{"CreateRollout", func() error {
			_, err := handler.CreateRollout(ctx, connect.NewRequest(&pb.CreateRolloutRequest{}))
			return err
		}},
		{"GetRollout", func() error {
			_, err := handler.GetRollout(ctx, connect.NewRequest(&pb.GetRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"ListRollouts", func() error {
			_, err := handler.ListRollouts(ctx, connect.NewRequest(&pb.ListRolloutsRequest{}))
			return err
		}},
		{"AdmitRollout", func() error {
			_, err := handler.AdmitRollout(ctx, connect.NewRequest(&pb.AdmitRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"ContinueRollout", func() error {
			_, err := handler.ContinueRollout(ctx, connect.NewRequest(&pb.ContinueRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"PauseRollout", func() error {
			_, err := handler.PauseRollout(ctx, connect.NewRequest(&pb.PauseRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"ResumeRollout", func() error {
			_, err := handler.ResumeRollout(ctx, connect.NewRequest(&pb.ResumeRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"AbortRollout", func() error {
			_, err := handler.AbortRollout(ctx, connect.NewRequest(&pb.AbortRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"RevertRollout", func() error {
			_, err := handler.RevertRollout(ctx, connect.NewRequest(&pb.RevertRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"CompleteRollout", func() error {
			_, err := handler.CompleteRollout(ctx, connect.NewRequest(&pb.CompleteRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
	}

	for _, testCase := range calls {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := testCase.call()
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
		})
	}
}

func TestHandlerSeparatesReadManageAndControlPermissions(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	service := &recordingRolloutService{
		result: &rolloutDomain.Rollout{
			ID:        rolloutID,
			OrgID:     42,
			State:     rolloutDomain.StateCreated,
			Revision:  1,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	handler := NewHandler(service, nil)

	readCtx := rolloutHandlerContext(t, 42, 9, authz.PermRolloutRead)
	response, err := handler.GetRollout(
		readCtx,
		connect.NewRequest(&pb.GetRolloutRequest{RolloutId: rolloutID.String()}),
	)
	require.NoError(t, err)
	assert.Equal(t, rolloutID.String(), response.Msg.GetRollout().GetRolloutId())
	assert.Equal(t, int64(42), service.getOrgID)

	_, err = handler.CreateRollout(
		readCtx,
		connect.NewRequest(&pb.CreateRolloutRequest{}),
	)
	assertPermissionDenied(t, err)

	manageCtx := rolloutHandlerContext(t, 42, 9, authz.PermRolloutManage)
	_, err = handler.PauseRollout(
		manageCtx,
		connect.NewRequest(&pb.PauseRolloutRequest{RolloutId: rolloutID.String()}),
	)
	assertPermissionDenied(t, err)

	controlCtx := rolloutHandlerContext(t, 42, 9, authz.PermRolloutControl)
	_, err = handler.GetRollout(
		controlCtx,
		connect.NewRequest(&pb.GetRolloutRequest{RolloutId: rolloutID.String()}),
	)
	assertPermissionDenied(t, err)
}

func TestHandlerDerivesControlOrganizationAndActorFromSession(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	service := &recordingRolloutService{
		result: &rolloutDomain.Rollout{
			ID:        rolloutID,
			OrgID:     73,
			State:     rolloutDomain.StatePaused,
			Revision:  3,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	handler := NewHandler(service, nil)
	ctx := rolloutHandlerContext(t, 73, 91, authz.PermRolloutControl)

	_, err := handler.PauseRollout(
		ctx,
		connect.NewRequest(&pb.PauseRolloutRequest{
			RolloutId:        rolloutID.String(),
			ExpectedRevision: 2,
			IdempotencyKey:   "pause-73",
			Reason:           "operator pause",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, int64(73), service.control.OrgID)
	assert.Equal(t, int64(91), service.control.ActorUserID)
	assert.Equal(t, rolloutID, service.control.RolloutID)
}

func TestHandlerDerivesAPIKeyControlActorFromSession(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	service := &recordingRolloutService{
		result: &rolloutDomain.Rollout{
			ID:        rolloutID,
			OrgID:     73,
			State:     rolloutDomain.StatePaused,
			Revision:  3,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	handler := NewHandler(service, nil)
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodAPIKey,
		APIKeyID:       "rollout-key-77",
		OrganizationID: 73,
		UserID:         91,
	})
	ctx = middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  []string{authz.PermRolloutControl},
	}}))

	_, err := handler.PauseRollout(
		ctx,
		connect.NewRequest(&pb.PauseRolloutRequest{
			RolloutId:        rolloutID.String(),
			ExpectedRevision: 2,
			IdempotencyKey:   "pause-api-key",
			Reason:           "automation pause",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.ActorTypeAPIKey, service.control.ActorType)
	require.NotNil(t, service.control.ActorCredentialID)
	assert.Equal(t, "apikey:rollout-key-77", *service.control.ActorCredentialID)
	assert.Equal(t, int64(91), service.control.ActorUserID)
}

type recordingRolloutService struct {
	result   *rolloutDomain.Rollout
	getOrgID int64
	control  rolloutDomain.ControlRequest
}

func (s *recordingRolloutService) Create(
	context.Context,
	rolloutDomain.CreateRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Get(
	_ context.Context,
	orgID int64,
	_ uuid.UUID,
) (*rolloutDomain.Rollout, error) {
	s.getOrgID = orgID
	return s.result, nil
}

func (s *recordingRolloutService) List(
	context.Context,
	int64,
	[]rolloutDomain.State,
) ([]rolloutDomain.Rollout, error) {
	return []rolloutDomain.Rollout{*s.result}, nil
}

func (s *recordingRolloutService) Admit(
	context.Context,
	rolloutDomain.AdmitRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Continue(
	context.Context,
	rolloutDomain.AdmitRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Pause(
	_ context.Context,
	req rolloutDomain.ControlRequest,
) (*rolloutDomain.Rollout, error) {
	s.control = req
	return s.result, nil
}

func (s *recordingRolloutService) Resume(
	context.Context,
	rolloutDomain.ControlRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Abort(
	context.Context,
	rolloutDomain.ControlRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Revert(
	context.Context,
	rolloutDomain.ControlRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Complete(
	context.Context,
	rolloutDomain.ControlRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func rolloutHandlerContext(
	t *testing.T,
	orgID int64,
	userID int64,
	permissions ...string,
) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{
		OrganizationID: orgID,
		UserID:         userID,
	})
	effective := authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  permissions,
	}})
	return middleware.WithEffectivePermissions(ctx, effective)
}

func assertPermissionDenied(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}
