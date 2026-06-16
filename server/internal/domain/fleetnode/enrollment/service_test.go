package enrollment

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterFleetNode_NormalizesMissingMinerSigningPubkeyToEmptyBytes(t *testing.T) {
	t.Parallel()

	// Arrange
	store := &registerFleetNodeStore{}
	svc := NewService(store, nil, inlineTransactor{}, nil)

	// Act
	agent, _, err := svc.RegisterFleetNode(t.Context(), "enroll-code", "node-1", []byte("identity"), nil)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, agent)
	assert.NotNil(t, store.gotMinerSigningPubkey)
	assert.Empty(t, store.gotMinerSigningPubkey)
	assert.NotNil(t, agent.MinerSigningPubkey)
	assert.Empty(t, agent.MinerSigningPubkey)
}

type inlineTransactor struct{}

func (inlineTransactor) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (inlineTransactor) RunInTxWithResult(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	return fn(ctx)
}

type registerFleetNodeStore struct {
	gotMinerSigningPubkey []byte
}

func (s *registerFleetNodeStore) CreatePendingEnrollment(context.Context, string, int64, int64, time.Time) (*PendingEnrollment, error) {
	panic("unexpected CreatePendingEnrollment")
}

func (s *registerFleetNodeStore) GetPendingEnrollmentByCodeHash(context.Context, string) (*PendingEnrollment, error) {
	return &PendingEnrollment{
		ID:        7,
		OrgID:     11,
		Status:    StatusPending,
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (s *registerFleetNodeStore) GetPendingEnrollmentByFleetNode(context.Context, int64, int64) (*PendingEnrollment, error) {
	panic("unexpected GetPendingEnrollmentByFleetNode")
}

func (s *registerFleetNodeStore) BindEnrollmentToFleetNode(context.Context, int64, int64) (int64, error) {
	return 1, nil
}

func (s *registerFleetNodeStore) ConfirmEnrollment(context.Context, int64, time.Time) (int64, error) {
	panic("unexpected ConfirmEnrollment")
}

func (s *registerFleetNodeStore) CancelPendingEnrollment(context.Context, int64, int64, time.Time) (int64, error) {
	panic("unexpected CancelPendingEnrollment")
}

func (s *registerFleetNodeStore) CancelEnrollmentForFleetNode(context.Context, int64, int64, time.Time) (int64, error) {
	panic("unexpected CancelEnrollmentForFleetNode")
}

func (s *registerFleetNodeStore) SweepExpiredEnrollments(context.Context, time.Time) (int64, error) {
	panic("unexpected SweepExpiredEnrollments")
}

func (s *registerFleetNodeStore) CreateFleetNode(_ context.Context, orgID int64, name string, identityPubkey, minerSigningPubkey []byte) (*FleetNode, error) {
	s.gotMinerSigningPubkey = minerSigningPubkey
	return &FleetNode{
		ID:                 99,
		OrgID:              orgID,
		Name:               name,
		IdentityPubkey:     identityPubkey,
		MinerSigningPubkey: minerSigningPubkey,
		EnrollmentStatus:   FleetNodeStatusPending,
	}, nil
}

func (s *registerFleetNodeStore) GetFleetNodeByID(context.Context, int64, int64) (*FleetNode, error) {
	panic("unexpected GetFleetNodeByID")
}

func (s *registerFleetNodeStore) GetFleetNodeByIDUnscoped(context.Context, int64) (*FleetNode, error) {
	panic("unexpected GetFleetNodeByIDUnscoped")
}

func (s *registerFleetNodeStore) LockFleetNodeByID(context.Context, int64, int64) (*FleetNode, error) {
	panic("unexpected LockFleetNodeByID")
}

func (s *registerFleetNodeStore) ListFleetNodesForOrganization(context.Context, int64) ([]FleetNodeListing, error) {
	panic("unexpected ListFleetNodesForOrganization")
}

func (s *registerFleetNodeStore) SetFleetNodeEnrollmentStatus(context.Context, FleetNodeStatus, int64, int64) (int64, error) {
	panic("unexpected SetFleetNodeEnrollmentStatus")
}

func (s *registerFleetNodeStore) SoftDeleteFleetNode(context.Context, int64, int64, time.Time) (int64, error) {
	panic("unexpected SoftDeleteFleetNode")
}

func (s *registerFleetNodeStore) SoftDeleteFleetNodesForExpiredEnrollments(context.Context, time.Time) (int64, error) {
	panic("unexpected SoftDeleteFleetNodesForExpiredEnrollments")
}

func (s *registerFleetNodeStore) UpdateLastSeen(context.Context, int64, int64, time.Time) (int64, error) {
	panic("unexpected UpdateLastSeen")
}

func (s *registerFleetNodeStore) DeletePairingsForFleetNode(context.Context, int64, int64) (int64, error) {
	panic("unexpected DeletePairingsForFleetNode")
}
