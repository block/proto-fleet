package poolconfigurations

import (
	"context"
	"database/sql"
	"testing"

	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

type selectionAuthority struct{ principal Principal }

func (a selectionAuthority) PoolEquipmentVisibility(context.Context, sqlc.Querier, Principal) (func(sql.NullInt64) bool, error) {
	return func(sql.NullInt64) bool { return true }, nil
}

func (a selectionAuthority) AuthorizePoolConfiguration(context.Context, sqlc.Querier, string, Access) (Principal, error) {
	return a.principal, nil
}

type selectionEquipment struct{ reviewAuthority }

func (selectionEquipment) VerifyPoolEquipmentCredentials(context.Context, string, string) (CredentialProof, error) {
	return func(context.Context, sqlc.Querier) error { return nil }, nil
}

type selectionResolver struct {
	identifiers []string
	calls       int
	filter      *fleetpb.MinerListFilter
}

func (s *selectionResolver) ResolveDeviceIdentifiers(_ context.Context, selector *fleetpb.DeviceSelector, _ int64) ([]string, error) {
	s.calls++
	s.filter = selector.GetAllDevices()
	return s.identifiers, nil
}

func TestReviewSelectionFreezesFilteredTargetsWithoutMutatingFilter(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	var identifier string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT device_identifier FROM device WHERE id=$1`, target.ID).Scan(&identifier))
	configuration, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
		return repo.Create(ctx, q, scope, RevisionOptions{Name: "Production", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
	})
	require.NoError(t, err)
	equipment := &selectionEquipment{reviewAuthority{binding: "frozen"}}
	resolver := &selectionResolver{identifiers: []string{identifier}}
	service := NewService(conn, repo, selectionAuthority{Principal{Scope: scope, ActorID: 1}}, equipment).WithDeviceSelector(resolver)
	filter := &fleetpb.MinerListFilter{GroupIds: []int64{42}}
	review, err := service.ReviewSelection(ctx, "", configuration.ID, configuration.Version, nil, nil, filter)
	require.NoError(t, err)
	require.Equal(t, []AssignmentTarget{target}, review.Targets)
	require.Empty(t, filter.PairingStatuses, "review must not mutate a caller's filter")
	require.Equal(t, []int64{42}, resolver.filter.GroupIds)
	require.Equal(t, []fleetpb.PairingStatus{fleetpb.PairingStatus_PAIRING_STATUS_PAIRED, fleetpb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD}, resolver.filter.PairingStatuses)
	resolver.identifiers = nil
	accepted, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (ReviewedSave, error) {
		return repo.ConfirmReview(ctx, q, scope, 1, review.ID, equipment)
	})
	require.NoError(t, err)
	require.Len(t, accepted.Operation.Targets, 1)
	require.Equal(t, target.ID, accepted.Operation.Targets[0].ID)
	require.Equal(t, 1, resolver.calls, "confirmation must not expand the filter again")
	_, err = service.ReviewSelection(ctx, "customer", configuration.ID, configuration.Version, nil, nil, filter)
	require.Error(t, err, "hosted scopes cannot resolve an operator-wide selection")
	_, err = service.ReviewSelection(ctx, "", configuration.ID, configuration.Version, []string{identifier}, nil, filter)
	require.Error(t, err, "a request cannot silently ignore either selection")
	review, err = service.ReviewSelection(ctx, "", configuration.ID, configuration.Version, nil, []int64{target.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, []AssignmentTarget{target}, review.Targets)
	for _, ids := range [][]int64{{target.ID, target.ID}, {0}, {-1}} {
		_, err = service.ReviewSelection(ctx, "", configuration.ID, configuration.Version, nil, ids, nil)
		require.Error(t, err)
	}
	_, err = service.ReviewSelection(ctx, "", configuration.ID, configuration.Version, []string{identifier}, []int64{target.ID}, nil)
	require.Error(t, err)
	_, err = service.ReviewSelection(ctx, "", configuration.ID, configuration.Version, nil, []int64{target.ID}, filter)
	require.Error(t, err)
}
