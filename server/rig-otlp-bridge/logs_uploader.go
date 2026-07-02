package main

import (
	"context"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

const logBatchFlushInterval = time.Second

type queuedLogBatch struct {
	rigAddress string
	payload    []byte
	labels     map[string]string
}

type logsUploader struct {
	endpoint      string
	httpClient    *http.Client
	queue         chan queuedLogBatch
	queuedBytes   atomic.Int64
	gzipEnabled   bool
	flushInterval time.Duration
}

func newLogsUploader(endpoint string, queueCapacity int, gzipEnabled bool) *logsUploader {
	return &logsUploader{
		endpoint:      endpoint,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		queue:         make(chan queuedLogBatch, queueCapacity),
		gzipEnabled:   gzipEnabled,
		flushInterval: logBatchFlushInterval,
	}
}

func (u *logsUploader) enqueue(info *rigInfo, payload []byte) {
	batch := queuedLogBatch{
		rigAddress: info.address,
		payload:    payload,
		labels:     info.labels,
	}
	// Hard budget: reserve first, undo on rejection, so concurrent
	// streams cannot collectively retain more than the cap.
	if u.queuedBytes.Add(int64(len(payload))) > maxQueuedBytes {
		u.queuedBytes.Add(-int64(len(payload)))
		log.Printf(
			"logs queue byte budget exceeded; dropping newest batch rig=%s payload_bytes=%d queued_bytes=%d",
			info.address,
			len(payload),
			u.queuedBytes.Load(),
		)
		return
	}
	select {
	case u.queue <- batch:
	default:
		u.queuedBytes.Add(-int64(len(payload)))
		log.Printf(
			"logs queue full; dropping newest batch rig=%s payload_bytes=%d queue_depth=%d queue_capacity=%d",
			info.address,
			len(payload),
			len(u.queue),
			cap(u.queue),
		)
	}
}

func (u *logsUploader) run(ctx context.Context) {
	ticker := time.NewTicker(u.flushInterval)
	defer ticker.Stop()

	pending := make([]queuedLogBatch, 0, cap(u.queue))
	pendingBytes := 0
	flush := func(flushCtx context.Context) {
		if len(pending) == 0 {
			return
		}
		batches := pending[:len(pending):len(pending)]
		pending = pending[:0]
		pendingBytes = 0
		_, err := pushCombinedLogBatches(
			flushCtx,
			u.httpClient,
			u.endpoint,
			batches,
			u.gzipEnabled,
			u.flushInterval,
			len(u.queue),
			cap(u.queue),
		)
		if err != nil {
			log.Printf("logs push -> %s: %v", u.endpoint, err)
		}
		clear(batches)
	}

	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			for more := true; more; {
				pending, pendingBytes, more = u.drainBounded(pending, pendingBytes)
				flush(flushCtx)
			}
			cancel()
			return
		case batch := <-u.queue:
			u.queuedBytes.Add(-int64(len(batch.payload)))
			pending = append(pending, batch)
			pendingBytes += len(batch.payload)
			// Bound between-tick accumulation: a fast stream must not
			// grow pending unbounded before the next flush.
			if len(pending) >= cap(u.queue) || pendingBytes >= maxPendingFlushBytes {
				flush(ctx)
			}
		case <-ticker.C:
			for more := true; more; {
				pending, pendingBytes, more = u.drainBounded(pending, pendingBytes)
				flush(ctx)
			}
		}
	}
}

// drainBounded moves queued batches into pending until the queue is empty or
// the flush budget is hit, so one flush never combines an unbounded backlog.
func (u *logsUploader) drainBounded(batches []queuedLogBatch, pendingBytes int) ([]queuedLogBatch, int, bool) {
	for {
		if len(batches) >= cap(u.queue) || pendingBytes >= maxPendingFlushBytes {
			return batches, pendingBytes, true
		}
		select {
		case batch := <-u.queue:
			u.queuedBytes.Add(-int64(len(batch.payload)))
			batches = append(batches, batch)
			pendingBytes += len(batch.payload)
		default:
			return batches, pendingBytes, false
		}
	}
}
