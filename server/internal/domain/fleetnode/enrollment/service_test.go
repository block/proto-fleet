package enrollment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

func TestRegisterFleetNode_CreatesFleetNodeWithIdentityAndEncryptionKeys(t *testing.T) {
	t.Parallel()

	// Arrange
	store := &registerFleetNodeStore{}
	svc := NewService(store, nil, inlineTransactor{}, nil)
	encryptionPubkey := []byte("01234567890123456789012345678901")

	// Act
	agent, _, err := svc.RegisterFleetNode(t.Context(), "enroll-code", "node-1", []byte("identity"), encryptionPubkey)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, agent)
	assert.Equal(t, int64(11), store.gotOrgID)
	assert.Equal(t, "node-1", store.gotName)
	assert.Equal(t, []byte("identity"), store.gotIdentityPubkey)
	assert.Equal(t, []byte("identity"), agent.IdentityPubkey)
	assert.Equal(t, encryptionPubkey, agent.EncryptionPubkey)
}

func TestRegisterFleetNode_RequiresEncryptionPubkey(t *testing.T) {
	t.Parallel()

	// Arrange
	store := &registerFleetNodeStore{}
	svc := NewService(store, nil, inlineTransactor{}, nil)

	// Act
	_, _, err := svc.RegisterFleetNode(t.Context(), "enroll-code", "node-1", []byte("identity"), nil)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encryption public key")
}

func TestRevokeFleetNodeInvalidatesControlStreamAfterPersistence(t *testing.T) {
	tests := []struct {
		name    string
		pending *PendingEnrollment
		revoke  func(context.Context, *Service) error
	}{
		{
			name: "confirmed node",
			revoke: func(ctx context.Context, service *Service) error {
				return service.RevokeFleetNode(ctx, 42, 7)
			},
		},
		{
			name:    "awaiting enrollment",
			pending: &PendingEnrollment{ID: 99},
			revoke: func(ctx context.Context, service *Service) error {
				return service.RevokeFleetNodeForEnrollment(ctx, 42, 7, 99)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := control.NewRegistry()
			stream := registry.Register(42)
			store := &revokeFleetNodeStore{pending: test.pending}
			apiKeyService := apikey.NewService(revokeAPIKeyStore{}, nil)
			service := NewService(store, apiKeyService, inlineTransactor{}, nil)
			service.WithControlStreamInvalidator(registry.Disconnect)

			err := test.revoke(t.Context(), service)

			require.NoError(t, err)
			select {
			case <-stream.Done:
			default:
				t.Fatal("revoked Fleet Node's ControlStream remains connected")
			}
		})
	}
}

func TestRevokeFleetNodeFailurePreservesControlStream(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(42)
	service := NewService(nil, nil, failingTransactor{}, nil)
	service.WithControlStreamInvalidator(registry.Disconnect)

	err := service.RevokeFleetNode(t.Context(), 42, 7)

	require.EqualError(t, err, "persistence failed")
	select {
	case <-stream.Done:
		t.Fatal("failed revocation disconnected the Fleet Node's ControlStream")
	default:
	}
}

type inlineTransactor struct{}

func (inlineTransactor) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (inlineTransactor) RunInTxWithResult(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	return fn(ctx)
}

type failingTransactor struct{}

func (failingTransactor) RunInTx(context.Context, func(context.Context) error) error {
	return errors.New("persistence failed")
}

func (failingTransactor) RunInTxWithResult(context.Context, func(context.Context) (any, error)) (any, error) {
	return nil, errors.New("persistence failed")
}

type revokeFleetNodeStore struct {
	Store
	pending *PendingEnrollment
}

func (s *revokeFleetNodeStore) LockFleetNodeByID(context.Context, int64, int64) (*FleetNode, error) {
	return &FleetNode{ID: 42, OrgID: 7, Name: "test-node"}, nil
}

func (s *revokeFleetNodeStore) GetPendingEnrollmentByFleetNode(context.Context, int64, int64) (*PendingEnrollment, error) {
	if s.pending == nil {
		return nil, fleeterror.NewNotFoundError("pending enrollment not found")
	}
	return s.pending, nil
}

func (*revokeFleetNodeStore) SetFleetNodeEnrollmentStatus(context.Context, FleetNodeStatus, int64, int64) (int64, error) {
	return 1, nil
}

func (*revokeFleetNodeStore) CancelEnrollmentForFleetNode(context.Context, int64, int64, time.Time) (int64, error) {
	return 1, nil
}

func (*revokeFleetNodeStore) ListDeviceIDsForFleetNode(context.Context, int64, int64) ([]int64, error) {
	return nil, nil
}

func (*revokeFleetNodeStore) DeleteMinerCredentialsForFleetNode(context.Context, int64, int64) (int64, error) {
	return 0, nil
}

func (*revokeFleetNodeStore) DeletePairingsForFleetNode(context.Context, int64, int64) (int64, error) {
	return 0, nil
}

func (*revokeFleetNodeStore) SoftDeleteFleetNode(context.Context, int64, int64, time.Time) (int64, error) {
	return 1, nil
}

type revokeAPIKeyStore struct {
	stores.ApiKeyStore
}

func (revokeAPIKeyStore) RevokeApiKeysByFleetNodeID(context.Context, int64, int64, time.Time) ([]string, error) {
	return nil, nil
}

type registerFleetNodeStore struct {
	gotOrgID          int64
	gotName           string
	gotIdentityPubkey []byte
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

func (s *registerFleetNodeStore) CreateFleetNode(_ context.Context, orgID int64, name string, identityPubkey, encryptionPubkey []byte) (*FleetNode, error) {
	s.gotOrgID = orgID
	s.gotName = name
	s.gotIdentityPubkey = append([]byte(nil), identityPubkey...)
	return &FleetNode{
		ID:               99,
		OrgID:            orgID,
		Name:             name,
		IdentityPubkey:   identityPubkey,
		EncryptionPubkey: encryptionPubkey,
		EnrollmentStatus: FleetNodeStatusPending,
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

func (s *registerFleetNodeStore) ListDeviceIDsForFleetNode(context.Context, int64, int64) ([]int64, error) {
	panic("unexpected ListDeviceIDsForFleetNode")
}

func (s *registerFleetNodeStore) DeleteMinerCredentialsForFleetNode(context.Context, int64, int64) (int64, error) {
	panic("unexpected DeleteMinerCredentialsForFleetNode")
}
