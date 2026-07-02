package main

import (
	"context"
	"errors"
	"io"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	miner_rpc "github.com/block/proto-fleet/server/rig-otlp-bridge/internal/rigapi/minertelemetry"
)

// runMetricsStream owns the gRPC StreamMetrics lifecycle for a single rig:
// dial, stream, push, reconnect with bounded exponential backoff. Returns
// when ctx is cancelled.
func runMetricsStream(
	ctx context.Context,
	info *rigInfo,
	uploader *metricsUploader,
	reconnectInitial, reconnectMax time.Duration,
) {
	backoff := reconnectInitial

	for {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		err := streamMetricsOnce(ctx, info, uploader)
		if ctx.Err() != nil {
			return
		}
		// A stream that outlived the backoff ceiling was healthy; reset
		// so the next drop reconnects fast.
		if time.Since(started) > reconnectMax {
			backoff = reconnectInitial
		}
		if err != nil && !errors.Is(err, io.EOF) {
			log.Printf("metrics stream %s: %v (retry in %s)", info.address, err, backoff)
		} else {
			log.Printf("metrics stream %s ended (retry in %s)", info.address, backoff)
		}
		if !sleepWithCancel(ctx, backoff) {
			return
		}
		backoff *= 2
		if backoff > reconnectMax {
			backoff = reconnectMax
		}
	}
}

func streamMetricsOnce(
	ctx context.Context,
	info *rigInfo,
	uploader *metricsUploader,
) error {
	conn, err := dialTelemetry(info)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := miner_rpc.NewMinerTelemetryApiClient(conn)
	stream, err := client.StreamMetrics(ctx, &miner_rpc.StreamMetricsRequest{})
	if err != nil {
		return err
	}

	var lastEnqueue time.Time
	dropped := 0
	for {
		batch, err := stream.Recv()
		if err != nil {
			return err
		}
		// Per-rig floor: a flooding rig must not fill the shared queue
		// and starve other rigs' batches (normal cadence is ~10s).
		if since := time.Since(lastEnqueue); since < minBatchInterval {
			dropped++
			if dropped%100 == 1 {
				log.Printf("rig %s exceeds max batch rate; dropped %d batches", info.address, dropped)
			}
			continue
		}
		lastEnqueue = time.Now()
		uploader.enqueue(info, batch.GetOtlpPayload())
	}
}

// minBatchInterval caps per-rig ingest at 1 batch/s (10x the rig-driven
// cadence), bounding a hostile rig to ~4 MiB/s instead of line rate.
const minBatchInterval = time.Second

// dialTelemetry builds a plaintext gRPC channel to telemetry-service.
// maxOTLPPayloadBytes makes the per-message bound on untrusted rig batches
// explicit (grpc-go's default); the upload queue bounds total buffering.
const maxOTLPPayloadBytes = 4 << 20

func dialTelemetry(info *rigInfo) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		info.address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxOTLPPayloadBytes)),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  250 * time.Millisecond,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   2 * time.Second,
			},
			MinConnectTimeout: 5 * time.Second,
		}),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
}
