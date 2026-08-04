package command

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestStreamCommandBatchUpdatesMapsContextCancellationToCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	responseChan := make(chan *pb.StreamCommandBatchUpdatesResponse)
	err := streamCommandBatchUpdates(ctx, responseChan, nil)

	require.ErrorIs(t, err, fleeterror.NewCanceledError())
}
