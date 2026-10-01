package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testWebIndex = "<!doctype html><html>production ProtoOS</html>"

func writeWebFile(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testWebRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		"index.html":                  testWebIndex,
		"config.js":                   "window.PROTO_CONFIG = {};",
		"version.txt":                 "test-build",
		"favicon.png":                 "test-icon",
		"assets/protoOS-12345678.js":  "console.log('production asset');",
		"assets/site.css":             "body { color: black; }",
		"fonts/example.woff2":         "test-font",
		"assets/directory/index.html": "not a directory listing",
		"private.txt":                 "not public",
		"api/v2/unknown":              "not an API response",
		"health/missing":              "not health",
		"metrics":                     "not public metrics",
		"debug/index.html":            "not public debug",
	} {
		writeWebFile(t, root, name, content)
	}
	return root
}

func TestWebRootValidation(t *testing.T) {
	for _, name := range []string{"valid", "missing root", "root file", "missing index", "directory index", "empty index", "escaping index symlink"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			switch name {
			case "valid":
				writeWebFile(t, root, "index.html", testWebIndex)
			case "missing root":
				root = filepath.Join(root, "missing")
			case "root file":
				writeWebFile(t, root, "file", "not a directory")
				root = filepath.Join(root, "file")
			case "directory index":
				if err := os.Mkdir(filepath.Join(root, "index.html"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "empty index":
				writeWebFile(t, root, "index.html", "")
			case "escaping index symlink":
				outside := t.TempDir()
				writeWebFile(t, outside, "index.html", testWebIndex)
				if err := os.Symlink(filepath.Join(outside, "index.html"), filepath.Join(root, "index.html")); err != nil {
					t.Fatal(err)
				}
			}
			handler, err := newWebHandler(root)
			if name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				_ = handler.root.Close()
				return
			}
			if err == nil {
				_ = handler.root.Close()
				t.Fatal("invalid build accepted")
			}
			if !strings.Contains(err.Error(), "--web-root") {
				t.Errorf("startup error lacks option context: %v", err)
			}
		})
	}
}

func TestHTTPHandlerWebHosting(t *testing.T) {
	root := testWebRoot(t)
	for _, enabled := range []bool{false, true} {
		label, webRoot := "API only", ""
		if enabled {
			label, webRoot = "same origin", root
		}
		t.Run(label, func(t *testing.T) {
			state := newContainerState(testConfig(), time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
			state.now = func() time.Time { return state.StartTime }
			handler, closeHandler, err := newHTTPHandler(state, webRoot)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(closeHandler)
			server := httptest.NewServer(handler)
			t.Cleanup(server.Close)
			for _, tc := range []struct {
				method, path string
				status       int
				contentType  string
				contains     string
				allow        string
			}{
				{"GET", "/api/v2/system/info", 200, "application/json", `"product":"CT1"`, ""},
				{"POST", "/api/v2/system/info", 405, "application/json", "method not allowed", "GET, HEAD"},
				{"GET", "/api/v2/unknown", 404, "application/json", "not found", ""},
				{"POST", "/api/v2/unknown", 404, "application/json", "not found", ""},
				{"GET", "/api/v1/system/info", 404, "application/json", "not found", ""},
				{"GET", "/api", 404, "application/json", "not found", ""},
				{"GET", "/health", 200, "", "OK", ""},
				{"POST", "/health", 200, "", "OK", ""},
				{"GET", "/health/missing", 404, "application/json", "not found", ""},
				{"GET", "/metrics", 404, "application/json", "not found", ""},
				{"POST", "/debug/index.html", 404, "application/json", "not found", ""},
			} {
				t.Run(tc.method+" "+tc.path, func(t *testing.T) {
					request, err := http.NewRequest(tc.method, server.URL+tc.path, nil)
					if err != nil {
						t.Fatal(err)
					}
					request.Header.Set("Accept", "text/html")
					response, err := server.Client().Do(request)
					if err != nil {
						t.Fatal(err)
					}
					defer response.Body.Close()
					body, err := io.ReadAll(response.Body)
					if err != nil {
						t.Fatal(err)
					}
					if response.StatusCode != tc.status || !strings.Contains(string(body), tc.contains) {
						t.Errorf("status/body = %d %q, want %d containing %q", response.StatusCode, body, tc.status, tc.contains)
					}
					if tc.contentType != "" && response.Header.Get("Content-Type") != tc.contentType {
						t.Errorf("content-type = %q, want %q", response.Header.Get("Content-Type"), tc.contentType)
					}
					if response.Header.Get("Allow") != tc.allow {
						t.Errorf("allow = %q, want %q", response.Header.Get("Allow"), tc.allow)
					}
					if tc.contentType == "application/json" && tc.status >= 400 {
						var envelope errorResponse
						if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code != tc.status {
							t.Errorf("JSON error envelope = %q (%v)", body, err)
						}
					}
				})
			}
			for _, url := range []string{"/", "/tanks/1/modules/2/serial", "/assets/protoOS-12345678.js"} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest("GET", url, nil))
				want := http.StatusNotFound
				if enabled {
					want = http.StatusOK
				}
				if response.Code != want {
					t.Errorf("GET %s status = %d, want %d", url, response.Code, want)
				}
			}
		})
	}
}

func TestWebHandlerStaticAndSPARoutes(t *testing.T) {
	handler, err := newWebHandler(testWebRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handler.root.Close() })
	for _, tc := range []struct {
		method, path string
		status       int
		contains     string
	}{
		{"GET", "/", 200, testWebIndex},
		{"GET", "/index.html", 200, testWebIndex},
		{"HEAD", "/", 200, ""},
		{"GET", "/assets/protoOS-12345678.js", 200, "production asset"},
		{"HEAD", "/assets/protoOS-12345678.js", 200, ""},
		{"GET", "/assets//site.css", 200, "color: black"},
		{"GET", "/fonts/example.woff2", 200, "test-font"},
		{"GET", "/config.js", 200, "PROTO_CONFIG"},
		{"GET", "/version.txt", 200, "test-build"},
		{"GET", "/favicon.png", 200, "test-icon"},
		{"GET", "/tanks/1/modules/2/serial?refresh=1", 200, testWebIndex},
		{"HEAD", "/modules/1/2/serial", 200, ""},
		{"GET", "/settings//network/", 200, testWebIndex},
		{"GET", "/assets/missing.js", 404, ""},
		{"GET", "/assets/missing", 404, ""},
		{"GET", "/fonts/missing", 404, ""},
		{"GET", "/assets/", 404, ""},
		{"GET", "/assets/directory/", 404, ""},
		{"GET", "/tanks/missing.js", 404, ""},
		{"GET", "/unknown-route", 404, ""},
		{"GET", "/private.txt", 404, ""},
		{"GET", "/assets/../index.html", 404, ""},
		{"GET", "/assets/%2e%2e/index.html", 404, ""},
		{"GET", "/assets/.hidden", 404, ""},
		{"GET", "/assets/foo%5cbar", 404, ""},
		{"GET", "/assets/foo%00bar", 404, ""},
		{"POST", "/", 405, ""},
		{"PUT", "/assets/protoOS-12345678.js", 405, ""},
		{"DELETE", "/tanks/1", 405, ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.contains) {
				t.Errorf("status/body = %d %q, want %d containing %q", response.Code, response.Body.String(), tc.status, tc.contains)
			}
			if tc.status != 200 && strings.Contains(response.Body.String(), testWebIndex) {
				t.Error("error fell back to index.html")
			}
			if tc.method == "HEAD" && response.Body.Len() != 0 {
				t.Error("HEAD returned a body")
			}
			if tc.status == 405 && response.Header().Get("Allow") != "GET, HEAD" {
				t.Errorf("allow = %q", response.Header().Get("Allow"))
			}
			if tc.status == 200 {
				if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Error("missing static response headers")
				}
				if tc.contains == testWebIndex && !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
					t.Errorf("index MIME type = %q", response.Header().Get("Content-Type"))
				}
				if strings.HasSuffix(tc.path, ".js") && !strings.Contains(response.Header().Get("Content-Type"), "javascript") {
					t.Errorf("JavaScript MIME type = %q", response.Header().Get("Content-Type"))
				}
				if strings.HasSuffix(tc.path, ".css") && !strings.HasPrefix(response.Header().Get("Content-Type"), "text/css") {
					t.Errorf("CSS MIME type = %q", response.Header().Get("Content-Type"))
				}
			}
		})
	}
	for _, prefix := range []string{"hashrate", "efficiency", "power-usage", "temperature", "logs", "pools", "systems", "tanks", "modules", "onboarding", "settings"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/"+prefix+"/deep-link", nil))
		if response.Code != 200 || response.Body.String() != testWebIndex {
			t.Errorf("browser route %s did not serve index.html", prefix)
		}
	}
}

func TestWebHandlerRejectsEscapingSymlinks(t *testing.T) {
	root, outside := testWebRoot(t), t.TempDir()
	writeWebFile(t, outside, "secret.js", "outside web root")
	for name, target := range map[string]string{"escape.js": filepath.Join(outside, "secret.js"), "escape-dir": outside} {
		if err := os.Symlink(target, filepath.Join(root, "assets", name)); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := newWebHandler(root)
	if err != nil {
		t.Fatal(err)
	}
	defer handler.root.Close()
	for _, url := range []string{"/assets/escape.js", "/assets/escape-dir/secret.js"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", url, nil))
		if response.Code != 404 || strings.Contains(response.Body.String(), "outside web root") {
			t.Errorf("escaping symlink %s: %d %q", url, response.Code, response.Body.String())
		}
	}
}
