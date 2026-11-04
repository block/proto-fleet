package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

type minerEndpoint struct {
	state *MinerState
	http  *minerHTTPHandler
}

type minerFleet struct {
	miners        map[netip.Addr]minerEndpoint
	single        *minerEndpoint
	delay         time.Duration
	metrics       operationMetrics
	metricsCancel context.CancelFunc
	metricsDone   chan struct{}
}

func loadMinerFleet() (*minerFleet, error) {
	f := &minerFleet{miners: make(map[netip.Addr]minerEndpoint)}
	if value := os.Getenv("FAKE_MINER_RESPONSE_DELAY"); value != "" {
		delay, err := time.ParseDuration(value)
		if err != nil || delay < 0 {
			return nil, fmt.Errorf("invalid FAKE_MINER_RESPONSE_DELAY %q", value)
		}
		f.delay = delay
	}
	path := os.Getenv("FAKE_MINER_ADDRESSES_FILE")
	if path == "" {
		state, err := newMinerState(getIPAddr())
		if err != nil {
			return nil, err
		}
		f.single = &minerEndpoint{state, createHTTPHandler(state)}
		return f, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		value := strings.TrimSpace(scanner.Text())
		if value == "" {
			continue
		}
		ip, err := netip.ParseAddr(value)
		if err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() {
			return nil, fmt.Errorf("invalid miner IPv4 address %q", value)
		}
		if _, exists := f.miners[ip]; exists {
			return nil, fmt.Errorf("duplicate miner address %s", ip)
		}
		state, err := newMinerState(ip.String())
		if err != nil {
			return nil, err
		}
		bytes := ip.As4()
		state.SerialNumber = fmt.Sprintf("fake-antminer-%02x%02x%02x%02x", bytes[0], bytes[1], bytes[2], bytes[3])
		state.MacAddress = fmt.Sprintf("02:00:%02x:%02x:%02x:%02x", bytes[0], bytes[1], bytes[2], bytes[3])
		state.Hostname = "antminer-" + state.SerialNumber
		state.LoadMode = true
		state.telemetrySample = uint64(bytes[0])<<24 | uint64(bytes[1])<<16 | uint64(bytes[2])<<8 | uint64(bytes[3])
		f.miners[ip] = minerEndpoint{state, createHTTPHandler(state)}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(f.miners) == 0 {
		return nil, fmt.Errorf("miner address file is empty")
	}
	return f, nil
}

func (f *minerFleet) endpoint(address net.Addr) (minerEndpoint, bool) {
	if f.single != nil {
		return *f.single, true
	}
	if address == nil {
		return minerEndpoint{}, false
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return minerEndpoint{}, false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return minerEndpoint{}, false
	}
	endpoint, ok := f.miners[ip.Unmap()]
	return endpoint, ok
}

func waitResponse(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay == 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func (f *minerFleet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	operation := "http:unknown"
	response := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	defer func() { f.metrics.record(operation, time.Since(start), response.status >= 400) }()
	address, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	endpoint, ok := f.endpoint(address)
	if !ok {
		http.NotFound(response, r)
		return
	}
	if _, pattern := endpoint.http.Handler(r); pattern != "" {
		operation = "http:" + pattern
	}
	if err := waitResponse(r.Context(), f.delay); err != nil {
		response.status = http.StatusRequestTimeout
		return
	}
	if r.URL.Path == "/cgi-bin/stats.cgi" || r.URL.Path == "/cgi-bin/summary.cgi" {
		endpoint.state.advanceTelemetry()
	}
	endpoint.http.ServeHTTP(response, r)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (f *minerFleet) handleRPC(ctx context.Context, conn net.Conn) {
	endpoint, ok := f.endpoint(conn.LocalAddr())
	if !ok {
		f.metrics.record("rpc:unknown", 0, true)
		conn.Close()
		return
	}
	handleRPCRequest(ctx, conn, endpoint.state, f.delay, &f.metrics)
}

type operationStat struct {
	Requests     uint64  `json:"requests_total"`
	Errors       uint64  `json:"errors_total"`
	LatencySumMS float64 `json:"latency_sum_ms"`
}

type operationMetrics struct {
	mu         sync.Mutex
	operations map[string]operationStat
}

func (m *operationMetrics) record(operation string, duration time.Duration, failed bool) {
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
	stat.LatencySumMS += float64(duration) / float64(time.Millisecond)
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

func (f *minerFleet) startMetrics(ctx context.Context) error {
	path := os.Getenv("FAKE_MINER_METRICS_FILE")
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	ctx, f.metricsCancel = context.WithCancel(ctx)
	f.metricsDone = make(chan struct{})
	go func() {
		defer close(f.metricsDone)
		defer file.Close()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				if err := f.metrics.flush(file); err != nil {
					fmt.Fprintf(os.Stderr, "miner metrics: %v\n", err)
				}
				return
			case <-ticker.C:
				if err := f.metrics.flush(file); err != nil {
					fmt.Fprintf(os.Stderr, "miner metrics: %v\n", err)
				}
			}
		}
	}()
	return nil
}

func (f *minerFleet) stopMetrics() {
	if f.metricsCancel != nil {
		f.metricsCancel()
		<-f.metricsDone
	}
}
