package hostedaccounts_test

import (
	"testing"

	"buf.build/go/protovalidate"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAccountKeyDescendantLimitRejectsBeforeStorage(t *testing.T) {
	// No storage dependencies: exceeding the bound must fail before beginning a
	// transaction or acquiring the organization administration lock.
	keys := hostedaccounts.NewKeyService(nil, nil)
	_, err := keys.Create(t.Context(), 1, 1, uuid.NewString(), hostedaccounts.KeyOptions{
		Name: "Automation", Capabilities: hostedaccounts.View,
		DescendantAccountIDs: make([]string, 101),
	})
	require.Error(t, err)
	require.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestAccountKeyRequestBoundsDescendants(t *testing.T) {
	request := &pb.CreateAccountKeyRequest{
		AccountId: uuid.NewString(), Name: "Automation", Capabilities: uint64(hostedaccounts.View),
	}
	for range 100 {
		request.DescendantAccountIds = append(request.DescendantAccountIds, uuid.NewString())
	}
	require.NoError(t, protovalidate.Validate(request), "the maximum bounded selection is valid")
	request.DescendantAccountIds = append(request.DescendantAccountIds, uuid.NewString())
	require.Error(t, protovalidate.Validate(request), "an oversized selection must fail request validation")
}
