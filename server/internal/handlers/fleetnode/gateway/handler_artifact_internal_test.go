package gateway

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

func TestContextConnectErrorMapsCanceledSeparatelyFromDeadline(t *testing.T) {
	assert.Equal(t, connect.CodeCanceled, connect.CodeOf(contextConnectError(context.Canceled, "upload closed")))
	assert.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(contextConnectError(context.DeadlineExceeded, "upload closed")))
}

func TestCommandArtifactTransferContextCancelsWhenCommandEnds(t *testing.T) {
	commandDone := make(chan struct{})
	ctx, cancel := commandArtifactTransferContext(context.Background(), commandDone, time.Hour)
	defer cancel()

	close(commandDone)

	select {
	case <-ctx.Done():
		assert.ErrorIs(t, ctx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("transfer context did not cancel after command ended")
	}
}

func TestMapArtifactAdmissionErrorHandlesNoActiveStream(t *testing.T) {
	err := mapArtifactAdmissionError(control.ErrNoActiveStream)

	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	assert.ErrorIs(t, err, control.ErrNoActiveStream)
}

func TestCommandArtifactUploadReaderRejectsOversizedChunk(t *testing.T) {
	reader := commandArtifactUploadReader{
		receive: func() (*pb.UploadCommandArtifactRequest, error) {
			return &pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Chunk{
				Chunk: &pb.CommandArtifactChunk{Data: make([]byte, commandArtifactChunkSize+1)},
			}}, nil
		},
	}

	n, err := reader.Read(make([]byte, 1))

	require.Error(t, err)
	assert.Zero(t, n)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
}
