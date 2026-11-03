package main

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServerTLSUsesProvidedCertificate(t *testing.T) {
	// Arrange
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

	// Act
	status, body := rigRequest(t, client, "GET", base+"/api/v1/pairing/info", "", nil)

	// Assert
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
			// Arrange
			for key, value := range values {
				t.Setenv(key, value)
			}

			// Act
			err := startHTTPServer(t.Context(), NewMinerState("TEST", "02:00:00:00:00:00"), 0)

			// Assert
			if err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}

func TestDelayedWriteDeadlineDoesNotMutateMiner(t *testing.T) {
	// Arrange
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

	// Act
	if response, err := client.Do(req); err == nil {
		response.Body.Close()
		t.Fatal("delayed write unexpectedly completed before deadline")
	}

	// This read waits out the configured delay and observes whether the canceled
	// write was incorrectly applied after its client disappeared.
	status, body := rigRequest(t, client, "GET", base+"/api/v1/mining", token, nil)

	// Assert
	var mining MiningStatus
	if err := json.Unmarshal(body, &mining); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || mining.MiningStatus.Status != "Mining" {
		t.Fatalf("canceled write mutated state: %d %s", status, body)
	}
}

func TestResponseDelayCancellationHappensBeforeDispatch(t *testing.T) {
	// Arrange
	called := false
	handler := instrumentMinerHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), time.Hour, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequest("POST", "/api/v1/pools", nil).WithContext(ctx)

	// Act
	start := time.Now()
	handler.ServeHTTP(httptest.NewRecorder(), request)

	// Assert
	if called || time.Since(start) > time.Second {
		t.Fatal("canceled delay dispatched or failed to return promptly")
	}
}

func TestMetricsAreBoundedAggregatesAndFlushAtShutdown(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "metrics.jsonl")
	metrics, err := newMinerMetrics(path)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pools/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	handler := instrumentMinerHandler(mux, 0, metrics)

	// Act
	for i := 0; i < 100; i++ {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/pools/"+strings.Repeat("x", i+1), nil))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/unrecognized/"+strings.Repeat("x", i+1), nil))
	}
	metrics.close()

	// Assert
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

type hijackableRecorder struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (r hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return r.conn, nil, nil
}

func TestHijackedRequestsAreRecordedAsFailures(t *testing.T) {
	// Arrange
	metrics, err := newMinerMetrics(filepath.Join(t.TempDir(), "metrics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(metrics.close)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/system", func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	})
	server, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	writer := hijackableRecorder{ResponseRecorder: httptest.NewRecorder(), conn: server}

	// Act
	instrumentMinerHandler(mux, 0, metrics).ServeHTTP(writer, httptest.NewRequest("GET", "/api/v1/system", nil))

	// Assert
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if value := metrics.operations["http:/api/v1/system"]; value.Requests != 1 || value.Errors != 1 {
		t.Fatalf("hijacked request must count as a failure: %+v", value)
	}
}

func TestShutdownJoinsHandlersBeforeFinalMetrics(t *testing.T) {
	for _, hijack := range []bool{false, true} {
		name := "forced close"
		if hijack {
			name = "hijacked connection"
		}
		t.Run(name, func(t *testing.T) {
			// Arrange
			path := filepath.Join(t.TempDir(), "metrics.jsonl")
			metrics, err := newMinerMetrics(path)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			releaseHandler := sync.OnceFunc(func() { close(release) })
			t.Cleanup(releaseHandler)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if hijack {
					conn, _, err := http.NewResponseController(w).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
				}
				close(entered)
				if hijack {
					<-ctx.Done()
				} else {
					<-r.Context().Done()
				}
				close(canceled)
				<-release
			})
			done := make(chan error, 1)
			go func() {
				err := serveHTTPServer(ctx, instrumentMinerHandler(handler, 0, metrics), listener, time.Millisecond)
				metrics.close()
				done <- err
			}()
			connection, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if _, err := io.WriteString(connection, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("request did not enter handler")
			}

			// Act
			cancel()

			// Assert
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("forced shutdown did not cancel request")
			}
			select {
			case err := <-done:
				t.Fatalf("server returned before handler metrics were recorded: %v", err)
			case <-time.After(25 * time.Millisecond):
			}

			// Act
			releaseHandler()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("server did not finish after handler returned")
			}

			// Assert
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var final struct {
				Requests uint64 `json:"requests_total"`
				Errors   uint64 `json:"errors_total"`
			}
			if err := json.Unmarshal(data, &final); err != nil {
				t.Fatal(err)
			}
			if final.Requests != 1 || (!hijack && final.Errors != 1) {
				t.Fatalf("final metrics lost accepted request or cancellation: %s", data)
			}
		})
	}
}
