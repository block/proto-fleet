package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"sync"
	"time"
)

type operationStat struct {
	Requests     uint64  `json:"requests_total"`
	Errors       uint64  `json:"errors_total"`
	LatencySumMS float64 `json:"latency_sum_ms"`
}

type operationMetrics struct {
	mu         sync.Mutex
	operations map[string]operationStat
}

func (m *operationMetrics) record(operation string, started time.Time, failed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operations == nil {
		m.operations = make(map[string]operationStat)
	}
	stat := m.operations[operation]
	stat.Requests++
	if failed {
		stat.Errors++
	}
	stat.LatencySumMS += float64(time.Since(started)) / float64(time.Millisecond)
	m.operations[operation] = stat
}

func (m *operationMetrics) flush(file *os.File) error {
	m.mu.Lock()
	operations := maps.Clone(m.operations)
	var requests, failures uint64
	for _, stat := range operations {
		requests += stat.Requests
		failures += stat.Errors
	}
	snapshot := struct {
		Timestamp  string                   `json:"timestamp"`
		Requests   uint64                   `json:"requests_total"`
		Errors     uint64                   `json:"errors_total"`
		Operations map[string]operationStat `json:"operations"`
	}{time.Now().UTC().Format(time.RFC3339Nano), requests, failures, operations}
	m.mu.Unlock()
	return json.NewEncoder(file).Encode(snapshot)
}

func (s *ContainerState) startMetrics(ctx context.Context, path string) (func(), error) {
	if path == "" {
		return func() {}, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open miner metrics: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	flush := func() {
		if err := s.metrics.flush(file); err != nil {
			log.Printf("write miner metrics: %v", err)
		}
	}
	go func() {
		defer close(done)
		defer file.Close()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				flush()
				return
			case <-ticker.C:
				flush()
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}

type operationResponse struct {
	http.ResponseWriter
	status int
}

func (w *operationResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *operationResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *operationResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}
