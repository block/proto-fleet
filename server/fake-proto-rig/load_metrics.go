package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

type operationMetrics struct {
	Requests     uint64  `json:"requests_total"`
	Errors       uint64  `json:"errors_total"`
	LatencySumMS float64 `json:"latency_sum_ms"`
}

type minerMetrics struct {
	mu         sync.Mutex
	requests   uint64
	errors     uint64
	operations map[string]operationMetrics
	file       *os.File
	stop       chan struct{}
	done       chan struct{}
}

func newMinerMetrics(path string) (*minerMetrics, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open miner metrics: %w", err)
	}
	m := &minerMetrics{file: file, operations: make(map[string]operationMetrics), stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(m.done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.flush()
			case <-m.stop:
				m.flush()
				return
			}
		}
	}()
	return m, nil
}

func (m *minerMetrics) close() {
	if m == nil {
		return
	}
	close(m.stop)
	<-m.done
	if err := m.file.Close(); err != nil {
		log.Printf("close miner metrics: %v", err)
	}
}

func (m *minerMetrics) flush() {
	m.mu.Lock()
	operations := make(map[string]operationMetrics, len(m.operations))
	for key, value := range m.operations {
		operations[key] = value
	}
	snapshot := struct {
		Timestamp  string                      `json:"timestamp"`
		Requests   uint64                      `json:"requests_total"`
		Errors     uint64                      `json:"errors_total"`
		Operations map[string]operationMetrics `json:"operations"`
	}{time.Now().UTC().Format(time.RFC3339Nano), m.requests, m.errors, operations}
	m.mu.Unlock()
	if err := json.NewEncoder(m.file).Encode(snapshot); err != nil {
		log.Printf("write miner metrics: %v", err)
	}
}

func (m *minerMetrics) record(pattern string, elapsed time.Duration, failed bool) {
	if m == nil {
		return
	}
	// ServeMux assigns one of the fixture's fixed registered patterns. Unknown
	// paths and canceled requests share one bucket, without per-IP/path labels.
	if pattern == "" {
		pattern = "unknown"
	}
	key := "http:" + pattern
	m.mu.Lock()
	defer m.mu.Unlock()
	operation := m.operations[key]
	operation.Requests++
	operation.LatencySumMS += float64(elapsed) / float64(time.Millisecond)
	m.requests++
	if failed {
		operation.Errors++
		m.errors++
	}
	m.operations[key] = operation
}

type metricsResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *metricsResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *metricsResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *metricsResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *metricsResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func instrumentMinerHandler(next http.Handler, delay time.Duration, metrics *minerMetrics) http.Handler {
	if delay == 0 && metrics == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		response := &metricsResponseWriter{ResponseWriter: w}
		defer func() {
			metrics.record(r.Pattern, time.Since(start), response.status >= 400 || r.Context().Err() != nil)
		}()
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-r.Context().Done():
				return
			case <-timer.C:
			}
		}
		// Cancellation before dispatch must not mutate state or acquire miner locks.
		if r.Context().Err() != nil {
			return
		}
		next.ServeHTTP(response, r)
	})
}
