package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestCanceledLoadOperationRetainsItsRegisteredRoute(t *testing.T) {
	for _, mode := range []string{"single", "multiple"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange
			var handler http.Handler = newRESTAPIHandler(NewMinerState("single", "02:00:00:00:00:01"))
			if mode == "multiple" {
				multi, err := newMultiMinerHandler(addressFile(t, "127.0.0.1\n"))
				if err != nil {
					t.Fatal(err)
				}
				handler = multi
			}
			metrics, err := newMinerMetrics(filepath.Join(t.TempDir(), "metrics.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			defer metrics.close()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			ctx = context.WithValue(ctx, http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/mining/stop", nil).WithContext(ctx)

			// Act
			instrumentMinerHandler(handler, time.Second, metrics).ServeHTTP(httptest.NewRecorder(), request)

			// Assert
			metrics.mu.Lock()
			defer metrics.mu.Unlock()
			value := metrics.operations["http:/api/v1/mining/stop"]
			if value.Requests != 1 || value.Errors != 1 {
				t.Fatalf("canceled operation lost attribution: %+v", metrics.operations)
			}
			if metrics.operations["http:unknown"].Requests != 0 {
				t.Fatal("known route was attributed to unknown")
			}
		})
	}
}
