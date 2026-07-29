package command

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
)

func TestStreamCommandBatchUpdatesMapsContextCancellationToCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	responseChan := make(chan *pb.StreamCommandBatchUpdatesResponse)
	wrapped := interceptors.NewErrorMappingInterceptor().WrapStreamingHandler(
		func(ctx context.Context, _ connect.StreamingHandlerConn) error {
			return streamCommandBatchUpdates(ctx, responseChan, nil)
		},
	)

	err := wrapped(ctx, nil)

	require.Equal(t, connect.CodeCanceled, connect.CodeOf(err))
}
