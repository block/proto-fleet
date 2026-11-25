package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/handlers/minerproxy"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (r *RunCmd) handleMinerWeb(ctx context.Context, stream acker, commandID string, request *pb.MinerWebRequest, logger *slog.Logger) {
	// Start execution time on arrival, covering validation and miner HTTP I/O.
	// Forward validates the requested duration before any miner I/O.
	ctx, cancel := context.WithTimeout(ctx, min(request.GetTimeout().AsDuration(), runtimepolicy.MinerWebRequestTimeout))
	defer cancel()
	if ctx.Err() != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid embedded miner request", logger)
		return
	}
	if err := validateDialTarget(request.GetTarget()); err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid miner address", logger)
		return
	}
	r.webOnce.Do(func() {
		if r.webClient == nil {
			r.webClient = minerproxy.NewNodeClient(nil)
		}
		r.webSlots = make(chan struct{}, control.MaxConcurrentMinerWebRequestsPerNode)
	})
	select {
	case r.webSlots <- struct{}{}:
		defer func() { <-r.webSlots }()
	default:
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BUSY, "embedded miner capacity reached", logger)
		return
	}
	response, err := r.webClient.Forward(ctx, request, func(context.Context) (string, error) {
		if r.minerSecrets == nil {
			return "", errors.New("node credential provider unavailable")
		}
		bundle, err := r.minerSecrets.SecretBundle(request.GetTarget())
		if err != nil {
			return "", err
		}
		credentials, ok := bundle.Kind.(sdk.UsernamePassword)
		if !ok {
			return "", errors.New("unsupported miner credentials")
		}
		return credentials.Password, nil
	})
	if err != nil {
		if fleeterror.IsInvalidArgumentError(err) {
			r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid embedded miner request", logger)
			return
		}
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_INTERNAL, "embedded miner request failed", logger)
		return
	}
	if err := sendMinerWebResponse(stream, commandID, response); err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_INTERNAL, "embedded miner response did not complete", logger)
		return
	}
	r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_OK, "", logger)
}

func sendMinerWebResponse(stream acker, commandID string, response *http.Response) error {
	closed := false
	defer func() {
		if !closed {
			_ = response.Body.Close()
		}
	}()
	send := func(frame *pb.MinerWebResponseChunk) error {
		frame.CommandId = commandID
		return stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_MinerWebResponse{MinerWebResponse: frame}})
	}
	status := response.StatusCode
	if status < 200 || status > 599 {
		return errors.New("invalid miner response status")
	}
	head := &pb.MinerWebResponseHead{StatusCode: uint32(status), Headers: minerproxy.MinerWebResponseHeaders(response.Header)}
	if err := send(&pb.MinerWebResponseChunk{Part: &pb.MinerWebResponseChunk_Head{Head: head}}); err != nil {
		return err
	}
	buffer := make([]byte, control.MinerWebChunkBytes)
	total := 0
	for {
		n, err := response.Body.Read(buffer)
		if n > 0 {
			total += n
			if total > control.MaxMinerWebResponseBytes {
				return errors.New("miner response exceeds limit")
			}
			if sendErr := send(&pb.MinerWebResponseChunk{Part: &pb.MinerWebResponseChunk_Data{Data: append([]byte(nil), buffer[:n]...)}}); sendErr != nil {
				return sendErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read miner response: %w", err)
		}
	}
	closed = true
	if err := response.Body.Close(); err != nil {
		return fmt.Errorf("close miner response: %w", err)
	}
	return send(&pb.MinerWebResponseChunk{Part: &pb.MinerWebResponseChunk_End{End: true}})
}
