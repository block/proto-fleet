// otlp-bridge discovers mining rigs over telemetry-service's gRPC port
// (default `2123`), then opens persistent server-streaming connections to
// `telemetry-service` for OTLP metrics and OTLP logs.
// Discovery labels (`hostname`, `site`) are injected into every Resource
// before pushing to configured OTLP HTTP receivers. `hostname` is read from
// the rig REST API before telemetry upload starts.
//
// Configuration is JSON (passed with `--config`, `OTLP_BRIDGE_CONFIG`, or
// default `config.json`):
//
//	{
//	  "subnets": ["172.16.2.0/24"],
//	  "targets": ["host.docker.internal:2123"],
//	  "site": "lab-east",
//	  "telemetry_port": 2123,
//	  "api_port": 80,
//	  "logs_endpoint": "http://loki:3100/otlp/v1/logs",
//	  "log_severity": "warn",
//	  "metrics_gzip": true,
//	  "logs_gzip": true,
//	  "metric_queue_expected_rigs": 16,
//	  "metric_queue_publish_interval_s": 10,
//	  "metric_queue_buffer_windows": 3,
//	  "scan_interval_s": 30,
//	  "stream_reconnect_initial_s": 1,
//	  "stream_reconnect_max_s": 30,
//	  "probe_timeout_s": 1.5,
//	  "workers": 64
//	}
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	miner_rpc "github.com/block/proto-fleet/server/rig-otlp-bridge/internal/rigapi/minertelemetry"
)

// streamManager owns one pair of stream workers per discovered rig.
type streamManager struct {
	metricsEndpoint  string
	logsEndpoint     string
	metricsUploader  *metricsUploader
	logsUploader     *logsUploader
	reconnectInitial time.Duration
	reconnectMax     time.Duration
	minLogSeverity   miner_rpc.LogSeverity

	mu           sync.Mutex
	workers      map[string]context.CancelFunc
	uploaderDone sync.WaitGroup
}

func newStreamManager(cfg *Config, metricsEndpoint string) *streamManager {
	queueCapacity := cfg.metricQueueCapacity()
	log.Printf(
		"telemetry queues: capacity=%d expected_rigs=%d estimated_services_per_rig=%d buffer_windows=%d publish_interval=%.1fs",
		queueCapacity,
		cfg.MetricQueueExpectedRigs,
		estimatedServicesPerRig,
		cfg.MetricQueueBufferWindows,
		cfg.MetricQueuePublishIntervalS,
	)
	return &streamManager{
		metricsEndpoint:  metricsEndpoint,
		logsEndpoint:     cfg.LogsEndpoint,
		metricsUploader:  newMetricsUploader(metricsEndpoint, queueCapacity, cfg.metricsGzipEnabled()),
		logsUploader:     newLogsUploader(cfg.LogsEndpoint, queueCapacity, cfg.logsGzipEnabled()),
		reconnectInitial: secondsToDuration(cfg.StreamReconnectInitialS),
		reconnectMax:     secondsToDuration(cfg.StreamReconnectMaxS),
		minLogSeverity:   cfg.minLogSeverity(),
		workers:          make(map[string]context.CancelFunc),
	}
}

func (s *streamManager) run(ctx context.Context) {
	s.uploaderDone.Add(1)
	go func() {
		defer s.uploaderDone.Done()
		s.metricsUploader.run(ctx)
	}()
	if s.logsEndpoint != "" {
		s.uploaderDone.Add(1)
		go func() {
			defer s.uploaderDone.Done()
			s.logsUploader.run(ctx)
		}()
	}
}

func (s *streamManager) wait() {
	s.uploaderDone.Wait()
}

func (s *streamManager) start(parent context.Context, info *rigInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.workers[info.address]; exists {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.workers[info.address] = cancel

	go runMetricsStream(ctx, info, s.metricsUploader, s.reconnectInitial, s.reconnectMax)
	if s.logsEndpoint != "" {
		go runLogsStream(ctx, info, s.logsUploader, s.minLogSeverity, s.reconnectInitial, s.reconnectMax)
	}
}

func (s *streamManager) stop(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.workers[addr]; ok {
		cancel()
		delete(s.workers, addr)
	}
}

func secondsToDuration(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

// sleepWithCancel waits up to d for ctx to cancel. Returns true if the
// sleep completed, false if ctx was cancelled. Uses `time.NewTimer` +
// `Stop` so a cancellation doesn't leak the timer goroutine that the
// naive `time.After` pattern leaves running until the deadline.
func sleepWithCancel(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func main() {
	// The upstream `push` (MCAP backfill) subcommand is intentionally not
	// vendored; that lab tool stays in miner-firmware.
	runDaemon(os.Args[1:])
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("otlp-bridge", flag.ExitOnError)
	configPath := fs.String("config", envOrDefault("OTLP_BRIDGE_CONFIG", defaultConfigPath), "path to config JSON")
	otlpEndpoint := fs.String("otlp-endpoint", envOrDefault("OTLP_BRIDGE_OTLP_ENDPOINT", defaultMetricsOTLPEndpoint), "OTLP HTTP metrics receiver URL")
	telemetryPortOverride := fs.Int("telemetry-port", envOrDefaultInt("OTLP_BRIDGE_TELEMETRY_PORT", 0), "Override telemetry-service gRPC port (0 = use config)")
	apiPortOverride := fs.Int("api-port", envOrDefaultInt("OTLP_BRIDGE_API_PORT", 0), "Override miner-api-server REST API port for identity/hostname lookup (0 = use config)")
	apiSchemeOverride := fs.String("api-scheme", envOrDefault("OTLP_BRIDGE_API_SCHEME", ""), "Override miner-api-server REST API scheme, http or https (empty = use config / http)")
	logsEndpointOverride := fs.String("logs-endpoint", envOrDefault("OTLP_BRIDGE_LOGS_ENDPOINT", ""), "Override OTLP HTTP logs receiver URL (empty = disabled)")
	logSeverityOverride := fs.String("min-log-severity", envOrDefault("OTLP_BRIDGE_MIN_LOG_SEVERITY", ""), "Minimum log severity to forward: info, warn, or error (empty = use config / warn)")
	fleetAPIURLOverride := fs.String("fleet-api-url", envOrDefault("OTLP_BRIDGE_FLEET_API_URL", ""), "proto-fleet API base URL for fleet-managed targets + enrichment (empty = use config / subnet scan)")
	fleetAPITokenOverride := fs.String("fleet-api-token", envOrDefault("OTLP_BRIDGE_FLEET_API_TOKEN", ""), "Bearer token for the proto-fleet API")
	fleetTargetCIDRsOverride := fs.String("fleet-target-cidrs", envOrDefault("OTLP_BRIDGE_FLEET_TARGET_CIDRS", ""), "Comma-separated CIDRs fleet-sourced targets must fall within (empty = private ranges)")
	fleetInsecureHTTPOverride := fs.Bool("fleet-api-insecure-http", envOrDefault("OTLP_BRIDGE_FLEET_API_INSECURE_HTTP", "") == "true", "Allow plain http to a non-loopback fleet API")
	fleetTargetModelsOverride := fs.String("fleet-target-models", envOrDefault("OTLP_BRIDGE_FLEET_TARGET_MODELS", ""), "Comma-separated device models to stream from (empty = proto rig default)")
	expectedRigsOverride := fs.Int("metric-queue-expected-rigs", envOrDefaultInt("OTLP_BRIDGE_METRIC_QUEUE_EXPECTED_RIGS", 0), "Override expected rig count for upload queue sizing (0 = use config; fleet mode otherwise floors at the default, so set this at larger sites)")
	fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		// Fleet sidecar deployments are env-driven and ship no config
		// file: fall back to defaults when a fleet API URL was given.
		if os.IsNotExist(err) && *fleetAPIURLOverride != "" {
			cfg = &Config{}
			cfg.applyDefaults()
		} else {
			log.Fatalf("load config %s: %v", *configPath, err)
		}
	}
	if *telemetryPortOverride != 0 {
		cfg.TelemetryPort = *telemetryPortOverride
	}
	if *apiPortOverride != 0 {
		cfg.APIPort = *apiPortOverride
	}
	if *apiSchemeOverride != "" {
		cfg.APIScheme = *apiSchemeOverride
	}
	if *logsEndpointOverride != "" {
		cfg.LogsEndpoint = *logsEndpointOverride
	}
	if *logSeverityOverride != "" {
		cfg.LogSeverity = *logSeverityOverride
	}
	if *fleetAPIURLOverride != "" {
		cfg.FleetAPIURL = *fleetAPIURLOverride
	}
	if *fleetAPITokenOverride != "" {
		cfg.FleetAPIToken = *fleetAPITokenOverride
	}
	if *fleetTargetCIDRsOverride != "" {
		cfg.FleetTargetCIDRs = strings.Split(*fleetTargetCIDRsOverride, ",")
	}
	if *fleetInsecureHTTPOverride {
		cfg.FleetAPIInsecureHTTP = true
	}
	if *fleetTargetModelsOverride != "" {
		cfg.FleetTargetModels = strings.Split(*fleetTargetModelsOverride, ",")
	}
	if *expectedRigsOverride != 0 {
		cfg.MetricQueueExpectedRigs = *expectedRigsOverride
	}
	if err := cfg.validate(); err != nil {
		log.Fatalf("config invalid: %v", err)
	}
	if err := cfg.validateTargetSource(); err != nil {
		log.Fatalf("config invalid: %v", err)
	}

	if cfg.FleetAPIURL != "" {
		log.Printf("fleet target allowlist: %s", strings.Join(cfg.FleetTargetCIDRs, ","))
	}
	log.Printf(
		"config: fleet_api=%q subnets=%d targets=%d telemetry_port=%d api_port=%d scan=%.1fs reconnect=%.1f-%.1fs metrics=%s logs=%q log_severity=%s metrics_gzip=%t logs_gzip=%t",
		cfg.FleetAPIURL, len(cfg.Subnets), len(cfg.Targets), cfg.TelemetryPort, cfg.APIPort, cfg.ScanIntervalS,
		cfg.StreamReconnectInitialS, cfg.StreamReconnectMaxS,
		*otlpEndpoint, cfg.LogsEndpoint, cfg.LogSeverity, cfg.metricsGzipEnabled(), cfg.logsGzipEnabled(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		s := <-sigCh
		log.Printf("received %s; shutting down", s)
		cancel()
	}()

	var fleetSrc *fleetTargetSource
	if cfg.FleetAPIURL != "" {
		fleetSrc, err = newFleetTargetSource(cfg.FleetAPIURL, cfg.FleetAPIToken, cfg.FleetTargetModels, cfg.FleetAPIInsecureHTTP)
		if err != nil {
			log.Fatalf("fleet target source: %v", err)
		}
	}

	reg := newRegistry()
	streams := newStreamManager(cfg, *otlpEndpoint)
	streams.run(ctx)
	scanLoop(ctx, cfg, reg, streams, fleetSrc)
	streams.wait()
}

func envOrDefaultInt(key string, fallback int) int {
	v := envOrDefault(key, "")
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
