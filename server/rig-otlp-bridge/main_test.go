package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	miner_rpc "github.com/block/proto-fleet/server/rig-otlp-bridge/internal/rigapi/minertelemetry"
)

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("OTLP_BRIDGE_CONFIG", "/tmp/config.json")
	if got := envOrDefault("OTLP_BRIDGE_CONFIG", "config.json"); got != "/tmp/config.json" {
		t.Fatalf("envOrDefault = %q, want env value", got)
	}
}

func TestEnvOrDefaultIgnoresEmptyEnv(t *testing.T) {
	t.Setenv("OTLP_BRIDGE_CONFIG", " ")
	if got := envOrDefault("OTLP_BRIDGE_CONFIG", "config.json"); got != "config.json" {
		t.Fatalf("envOrDefault = %q, want fallback", got)
	}
}

func TestConfigDefaultsApply(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, []byte(`{"targets":["host.docker.internal"]}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.TelemetryPort != defaultTelemetryPort {
		t.Errorf("TelemetryPort = %d, want %d", cfg.TelemetryPort, defaultTelemetryPort)
	}
	if cfg.APIPort != defaultAPIPort {
		t.Errorf("APIPort = %d, want %d", cfg.APIPort, defaultAPIPort)
	}
	if !cfg.metricsGzipEnabled() {
		t.Error("MetricsGzip default = false, want true")
	}
	if !cfg.logsGzipEnabled() {
		t.Error("LogsGzip default = false, want true")
	}
	if cfg.MetricQueueExpectedRigs != defaultMetricExpectedRigsFloor {
		t.Errorf("MetricQueueExpectedRigs = %d, want %d", cfg.MetricQueueExpectedRigs, defaultMetricExpectedRigsFloor)
	}
	if cfg.MetricQueuePublishIntervalS != defaultMetricQueuePublishS {
		t.Errorf("MetricQueuePublishIntervalS = %f, want %f", cfg.MetricQueuePublishIntervalS, defaultMetricQueuePublishS)
	}
	if cfg.MetricQueueBufferWindows != defaultMetricQueueWindows {
		t.Errorf("MetricQueueBufferWindows = %d, want %d", cfg.MetricQueueBufferWindows, defaultMetricQueueWindows)
	}
}

func TestLogSeverityDefaultsToWarn(t *testing.T) {
	cfg := &Config{Targets: []string{"rig"}}
	cfg.applyDefaults()
	if cfg.LogSeverity != defaultLogSeverity {
		t.Errorf("LogSeverity = %q, want %q", cfg.LogSeverity, defaultLogSeverity)
	}
	if got := cfg.minLogSeverity(); got != miner_rpc.LogSeverity_LOG_SEVERITY_WARN {
		t.Errorf("minLogSeverity() = %v, want WARN", got)
	}
}

func TestLogSeverityNameMapping(t *testing.T) {
	cases := map[string]miner_rpc.LogSeverity{
		"info":    miner_rpc.LogSeverity_LOG_SEVERITY_INFO,
		"warn":    miner_rpc.LogSeverity_LOG_SEVERITY_WARN,
		"warning": miner_rpc.LogSeverity_LOG_SEVERITY_WARN,
		"ERROR":   miner_rpc.LogSeverity_LOG_SEVERITY_ERROR,
	}
	for name, want := range cases {
		got, err := severityNameToEnum(name)
		if err != nil {
			t.Errorf("severityNameToEnum(%q) error: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("severityNameToEnum(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestLogSeverityRejectsInvalid(t *testing.T) {
	cfg := &Config{Targets: []string{"rig"}, LogSeverity: "trace"}
	cfg.applyDefaults()
	if err := cfg.validate(); err == nil {
		t.Fatal("validate accepted log_severity=trace")
	}
}

func TestConfigTelemetryPortCanBeSetExplicitly(t *testing.T) {
	cfg := &Config{Targets: []string{"rig"}, TelemetryPort: 9000}
	cfg.applyDefaults()
	if cfg.TelemetryPort != 9000 {
		t.Errorf("TelemetryPort = %d, want 9000", cfg.TelemetryPort)
	}
}

func TestConfigAPIPortCanBeSetExplicitly(t *testing.T) {
	cfg := &Config{Targets: []string{"rig"}, APIPort: 8080}
	cfg.applyDefaults()
	if cfg.APIPort != 8080 {
		t.Errorf("APIPort = %d, want 8080", cfg.APIPort)
	}
}

func TestMetricQueueCapacityDefaults(t *testing.T) {
	cfg := &Config{Targets: []string{"rig-1"}}
	cfg.applyDefaults()
	if got := cfg.metricQueueCapacity(); got != 768 {
		t.Fatalf("metricQueueCapacity = %d, want 768", got)
	}
}

func TestMetricQueueCapacityUsesTargetCount(t *testing.T) {
	targets := make([]string, 20)
	for i := range targets {
		targets[i] = "rig-" + strconv.Itoa(i)
	}
	cfg := &Config{Targets: targets}
	cfg.applyDefaults()
	if got := cfg.metricQueueCapacity(); got != 960 {
		t.Fatalf("metricQueueCapacity = %d, want 960", got)
	}
}

func TestMetricQueueCapacityUsesExplicitExpectedRigs(t *testing.T) {
	cfg := &Config{Targets: []string{"rig"}, MetricQueueExpectedRigs: 40}
	cfg.applyDefaults()
	if got := cfg.metricQueueCapacity(); got != 1920 {
		t.Fatalf("metricQueueCapacity = %d, want 1920", got)
	}
}

func TestMetricQueueCapacityMinClamp(t *testing.T) {
	cfg := &Config{
		Targets:                  []string{"rig"},
		MetricQueueExpectedRigs:  1,
		MetricQueueBufferWindows: 1,
	}
	cfg.applyDefaults()
	if got := cfg.metricQueueCapacity(); got != minMetricQueueCapacity {
		t.Fatalf("metricQueueCapacity = %d, want %d", got, minMetricQueueCapacity)
	}
}

func TestMetricQueueCapacityMaxClamp(t *testing.T) {
	cfg := &Config{Targets: []string{"rig"}, MetricQueueExpectedRigs: 1000}
	cfg.applyDefaults()
	if got := cfg.metricQueueCapacity(); got != maxMetricQueueCapacity {
		t.Fatalf("metricQueueCapacity = %d, want %d", got, maxMetricQueueCapacity)
	}
}

func TestGzipCanBeDisabled(t *testing.T) {
	disabled := false
	cfg := &Config{
		Targets:     []string{"rig"},
		MetricsGzip: &disabled,
		LogsGzip:    &disabled,
	}
	cfg.applyDefaults()
	if cfg.metricsGzipEnabled() {
		t.Error("metricsGzipEnabled = true, want false")
	}
	if cfg.logsGzipEnabled() {
		t.Error("logsGzipEnabled = true, want false")
	}
}

func TestConfigRejectsLegacyMcddPortField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, []byte(`{"subnets":["10.0.0.0/24"],"mcdd_port":2121}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("loadConfig accepted legacy mcdd_port field")
	}
}

func TestConfigRejectsLegacyPortField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, []byte(`{"subnets":["10.0.0.0/24"],"port":8080}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("loadConfig accepted legacy port field")
	}
}

func TestConfigRejectsLegacyPublishInterval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, []byte(`{"subnets":["10.0.0.0/24"],"publish_interval_s":5}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("loadConfig accepted legacy publish_interval_s")
	}
}

func TestLoadConfigRejectsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, []byte(`{"targets":["rig"],"scan_interval_s":-1}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("loadConfig accepted invalid scan_interval_s")
	}
}

func newValidConfig() *Config {
	cfg := &Config{
		Subnets: []string{"10.0.0.0/24"},
	}
	cfg.applyDefaults()
	return cfg
}

func TestConfigValidateRejectsBadReconnectWindow(t *testing.T) {
	cfg := newValidConfig()
	cfg.StreamReconnectInitialS = 10
	cfg.StreamReconnectMaxS = 1
	if err := cfg.validate(); err == nil {
		t.Fatal("validate accepted initial > max reconnect window")
	}
}

func TestEnvOrDefaultIntParses(t *testing.T) {
	t.Setenv("OTLP_BRIDGE_TELEMETRY_PORT", "9000")
	if got := envOrDefaultInt("OTLP_BRIDGE_TELEMETRY_PORT", 2123); got != 9000 {
		t.Fatalf("envOrDefaultInt = %d, want 9000", got)
	}
}

func TestEnvOrDefaultIntFallsBackOnGarbage(t *testing.T) {
	t.Setenv("OTLP_BRIDGE_TELEMETRY_PORT", "not-a-number")
	if got := envOrDefaultInt("OTLP_BRIDGE_TELEMETRY_PORT", 2123); got != 2123 {
		t.Fatalf("envOrDefaultInt = %d, want fallback 2123", got)
	}
}

func TestParseTargetUsesDefaultPort(t *testing.T) {
	got, err := parseTarget("host.docker.internal", defaultTelemetryPort)
	if err != nil {
		t.Fatalf("parseTarget: %v", err)
	}
	if got.host != "host.docker.internal" || got.port != defaultTelemetryPort {
		t.Fatalf("parseTarget = %#v, want host.docker.internal:%d", got, defaultTelemetryPort)
	}
}

func TestParseTargetUsesExplicitPort(t *testing.T) {
	got, err := parseTarget("host.docker.internal:9090", defaultTelemetryPort)
	if err != nil {
		t.Fatalf("parseTarget: %v", err)
	}
	if got.host != "host.docker.internal" || got.port != 9090 {
		t.Fatalf("parseTarget = %#v, want host.docker.internal:9090", got)
	}
}

func TestParseTargetRejectsURL(t *testing.T) {
	if _, err := parseTarget("http://host.docker.internal:2123", defaultTelemetryPort); err == nil {
		t.Fatal("parseTarget accepted a URL")
	}
}

func TestParseTargetRejectsMissingHost(t *testing.T) {
	if _, err := parseTarget(":2123", defaultTelemetryPort); err == nil {
		t.Fatal("parseTarget accepted a target without a host")
	}
}

func TestBuildLabelsUsesRESTHostname(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/network" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"network-info":{"hostname":"rig-from-api"}}`))
	}))
	defer srv.Close()

	host, port := splitTestServer(t, srv)
	labels, err := buildLabels(context.Background(), host, "lab-east", "http", port)
	if err != nil {
		t.Fatalf("buildLabels: %v", err)
	}
	if labels["hostname"] != "rig-from-api" {
		t.Errorf("hostname = %q, want REST hostname", labels["hostname"])
	}
	if labels["rig_ip"] != host {
		t.Errorf("rig_ip = %q, want target host %q", labels["rig_ip"], host)
	}
	if labels["site"] != "lab-east" {
		t.Errorf("site = %q, want lab-east", labels["site"])
	}
	if _, ok := labels["cb_sn"]; ok {
		t.Error("cb_sn must not appear in labels")
	}
}

func TestBuildLabelsHonorsHTTPSScheme(t *testing.T) {
	// Self-signed TLS, like real proto rigs; rigAPIHTTPClient skips verify.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"network-info":{"hostname":"rig-https"}}`))
	}))
	defer srv.Close()

	host, port := splitTestServer(t, srv)
	labels, err := buildLabels(context.Background(), host, "lab-east", "https", port)
	if err != nil {
		t.Fatalf("buildLabels over https: %v", err)
	}
	if labels["hostname"] != "rig-https" {
		t.Errorf("hostname = %q, want rig-https", labels["hostname"])
	}
}

func TestBuildLabelsOmitsRigIPWhenTargetHostIsName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"network-info":{"hostname":"rig-from-api"}}`))
	}))
	defer srv.Close()

	_, port := splitTestServer(t, srv)
	labels, err := buildLabels(context.Background(), "localhost", "", "http", port)
	if err != nil {
		t.Fatalf("buildLabels: %v", err)
	}
	if _, ok := labels["rig_ip"]; ok {
		t.Errorf("rig_ip should be omitted for DNS targets, got %q", labels["rig_ip"])
	}
}

func TestBuildLabelsOmitsSiteWhenEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"network-info":{"hostname":"rig-from-api"}}`))
	}))
	defer srv.Close()

	host, port := splitTestServer(t, srv)
	labels, err := buildLabels(context.Background(), host, "", "http", port)
	if err != nil {
		t.Fatalf("buildLabels: %v", err)
	}
	if _, ok := labels["site"]; ok {
		t.Errorf("empty site should be omitted, got %q", labels["site"])
	}
}

func TestBuildLabelsFailsWhenRESTUnavailable(t *testing.T) {
	if _, err := buildLabels(context.Background(), "127.0.0.1", "", "http", 1); err == nil {
		t.Fatal("buildLabels succeeded when REST hostname lookup failed")
	}
}

func splitTestServer(t *testing.T, srv *httptest.Server) (string, int) {
	t.Helper()
	host, portString, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("Atoi(%q): %v", portString, err)
	}
	return host, port
}
