package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServerTLSUsesProvidedCertificate(t *testing.T) {
	reference := httptest.NewTLSServer(http.NotFoundHandler())
	defer reference.Close()
	certificate := reference.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(t.TempDir(), "cert.pem"), filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TLS_CERT_FILE", certPath)
	t.Setenv("TLS_KEY_FILE", keyPath)
	t.Setenv("FAKE_MINER_ADDRESSES_FILE", addressFile(t, "127.0.0.1\n"))
	base := startRigServer(t)
	client := reference.Client()
	defer client.CloseIdleConnections()
	status, body := rigRequest(t, client, "GET", base+"/api/v1/pairing/info", "", nil)
	if status != http.StatusOK {
		t.Fatalf("TLS pairing status %d: %s", status, body)
	}
}

func TestServerRejectsInvalidTLSAndDelayConfiguration(t *testing.T) {
	for _, values := range []map[string]string{
		{"TLS_CERT_FILE": "missing-cert"},
		{"TLS_KEY_FILE": "missing-key"},
		{"FAKE_MINER_RESPONSE_DELAY": "invalid"},
		{"FAKE_MINER_RESPONSE_DELAY": "-1ms"},
	} {
		t.Run("invalid configuration", func(t *testing.T) {
			for key, value := range values {
				t.Setenv(key, value)
			}
			if err := startHTTPServer(t.Context(), NewMinerState("TEST", "02:00:00:00:00:00"), 0); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}

func TestDelayedWriteDeadlineDoesNotMutateMiner(t *testing.T) {
	t.Setenv("FAKE_MINER_ADDRESSES_FILE", addressFile(t, "127.0.0.1\n"))
	t.Setenv("FAKE_MINER_RESPONSE_DELAY", "100ms")
	base := startRigServer(t)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	token := rigLogin(t, client, base, "unused")
	if status, body := rigRequest(t, client, "POST", base+"/api/v1/pools", token, strings.NewReader(`[{"url":"stratum+tcp://pool.example:3333","username":"worker"}]`)); status != http.StatusOK {
		t.Fatalf("seed pool: %d %s", status, body)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/v1/mining/stop", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if response, err := client.Do(req); err == nil {
		response.Body.Close()
		t.Fatal("delayed write unexpectedly completed before deadline")
	}
	// This read waits out the configured delay and observes whether the canceled
	// write was incorrectly applied after its client disappeared.
	status, body := rigRequest(t, client, "GET", base+"/api/v1/mining", token, nil)
	var mining MiningStatus
	if err := json.Unmarshal(body, &mining); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || mining.MiningStatus.Status != "Mining" {
		t.Fatalf("canceled write mutated state: %d %s", status, body)
	}
}

func TestResponseDelayCancellationHappensBeforeDispatch(t *testing.T) {
	called := false
	handler := instrumentMinerHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), time.Hour, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequest("POST", "/api/v1/pools", nil).WithContext(ctx)
	start := time.Now()
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if called || time.Since(start) > time.Second {
		t.Fatal("canceled delay dispatched or failed to return promptly")
	}
}

func TestMetricsAreBoundedAggregatesAndFlushAtShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.jsonl")
	metrics, err := newMinerMetrics(path)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pools/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	handler := instrumentMinerHandler(mux, 0, metrics)
	for i := 0; i < 100; i++ {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/pools/"+strings.Repeat("x", i+1), nil))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/unrecognized/"+strings.Repeat("x", i+1), nil))
	}
	metrics.close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Timestamp  string                      `json:"timestamp"`
		Requests   uint64                      `json:"requests_total"`
		Errors     uint64                      `json:"errors_total"`
		Operations map[string]operationMetrics `json:"operations"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Requests != 200 || record.Errors != 200 || len(record.Operations) != 2 {
		t.Fatalf("unbounded or incomplete metrics: %s", data)
	}
	if _, err := time.Parse(time.RFC3339Nano, record.Timestamp); err != nil || !strings.HasSuffix(record.Timestamp, "Z") {
		t.Fatalf("timestamp is not UTC: %q", record.Timestamp)
	}
	if value := record.Operations["http:/api/v1/pools/"]; value.Requests != 100 || value.Errors != 100 || value.LatencySumMS <= 0 {
		t.Fatalf("incorrect operation aggregate: %+v", value)
	}
	if record.Operations["http:unknown"].Requests != 100 {
		t.Fatalf("unknown paths not grouped: %s", data)
	}
}
