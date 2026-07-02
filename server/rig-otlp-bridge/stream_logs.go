package main

import (
	"context"
	"errors"
	"io"
	"log"
	"time"

	miner_rpc "github.com/block/proto-fleet/server/rig-otlp-bridge/internal/rigapi/minertelemetry"
)

// runLogsStream owns the gRPC StreamLogs lifecycle for a single rig.
func runLogsStream(
	ctx context.Context,
	info *rigInfo,
	uploader *logsUploader,
	minSeverity miner_rpc.LogSeverity,
	reconnectInitial, reconnectMax time.Duration,
) {
	backoff := reconnectInitial

	for {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		err := streamLogsOnce(ctx, info, uploader, minSeverity)
		if ctx.Err() != nil {
			return
		}
		// A stream that outlived the backoff ceiling was healthy; reset
		// so the next drop reconnects fast.
		if time.Since(started) > reconnectMax {
			backoff = reconnectInitial
		}
		if err != nil && !errors.Is(err, io.EOF) {
			log.Printf("logs stream %s: %v (retry in %s)", info.address, err, backoff)
		} else {
			log.Printf("logs stream %s ended (retry in %s)", info.address, backoff)
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

func streamLogsOnce(
	ctx context.Context,
	info *rigInfo,
	uploader *logsUploader,
	minSeverity miner_rpc.LogSeverity,
) error {
	conn, err := dialTelemetry(info)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := miner_rpc.NewMinerTelemetryApiClient(conn)
	stream, err := client.StreamLogs(ctx, &miner_rpc.StreamLogsRequest{MinSeverity: minSeverity})
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
				log.Printf("rig %s exceeds max log batch rate; dropped %d batches", info.address, dropped)
			}
			continue
		}
		lastEnqueue = time.Now()
		uploader.enqueue(info, batch.GetOtlpPayload())
	}
}
