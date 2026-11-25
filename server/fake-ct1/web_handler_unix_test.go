//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func makeWebFIFO(t *testing.T, filename string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filename, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Bound the regression check and unblock a mistaken FIFO Open before failing,
// so restoring the bug cannot leave the test suite hanging on a named pipe.
func assertFIFOOperationCompletes(t *testing.T, filename string, operation func() error) {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- operation() }()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
		return
	case <-timer.C:
	}

	fd, err := syscall.Open(filename, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("operation blocked on FIFO; could not unblock it: %v", err)
	}
	defer syscall.Close(fd)
	timer.Reset(2 * time.Second)
	select {
	case <-result:
		t.Fatal("operation blocked until the FIFO was opened for writing")
	case <-timer.C:
		t.Fatal("operation did not complete after unblocking the FIFO")
	}
}

func TestWebRootRejectsFIFOIndex(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		name := "FIFO"
		if symlink {
			name = "symlink to FIFO"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			filename := filepath.Join(root, "index.html")
			fifo := filename
			if symlink {
				fifo = filepath.Join(root, "index.fifo")
				if err := os.Symlink(filepath.Base(fifo), filename); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(fifo, []byte(testWebIndex), 0o600); err != nil {
				t.Fatal(err)
			}
			handler, err := newWebHandler(root)
			if err != nil {
				t.Fatalf("regular index rejected: %v", err)
			}
			_ = handler.root.Close()
			// Replace the validated regular file atomically, including a symlink target.
			replacement := fifo + ".replacement"
			makeWebFIFO(t, replacement)
			if err := os.Rename(replacement, fifo); err != nil {
				t.Fatal(err)
			}
			assertFIFOOperationCompletes(t, fifo, func() error {
				handler, err := newWebHandler(root)
				if handler != nil {
					_ = handler.root.Close()
				}
				if err == nil {
					return fmt.Errorf("FIFO index accepted")
				}
				if !strings.Contains(err.Error(), "--web-root") {
					return fmt.Errorf("startup error lacks option context: %w", err)
				}
				return nil
			})
		})
	}
}

func TestWebHandlerRejectsFIFOFiles(t *testing.T) {
	for _, tc := range []struct {
		name, file, url string
		symlink         bool
	}{
		{"index", "index.html", "/index.html", false},
		{"SPA fallback index", "index.html", "/tanks/1", false},
		{"public file", "config.js", "/config.js", false},
		{"asset", "assets/pipe.js", "/assets/pipe.js", false},
		{"font", "fonts/pipe.woff2", "/fonts/pipe.woff2", false},
		{"asset symlink", "assets/pipe.js", "/assets/pipe.js", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testWebRoot(t)
			filename := filepath.Join(root, tc.file)
			if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			fifo := filename
			if tc.symlink {
				fifo += ".fifo"
				if err := os.Symlink(filepath.Base(fifo), filename); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Dir(fifo), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fifo, []byte(testWebIndex), 0o600); err != nil {
				t.Fatal(err)
			}
			handler, err := newWebHandler(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = handler.root.Close() })
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(method, tc.url, nil))
				if response.Code != http.StatusOK {
					t.Fatalf("regular file response status = %d, want 200", response.Code)
				}
				if method == http.MethodGet && response.Body.String() != testWebIndex {
					t.Fatalf("regular file response body = %q, want %q", response.Body.String(), testWebIndex)
				}
			}
			// Atomically replace the served file without changing the handler or symlink.
			replacement := fifo + ".replacement"
			makeWebFIFO(t, replacement)
			if err := os.Rename(replacement, fifo); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				t.Run(method, func(t *testing.T) {
					assertFIFOOperationCompletes(t, fifo, func() error {
						response := httptest.NewRecorder()
						handler.ServeHTTP(response, httptest.NewRequest(method, tc.url, nil))
						if response.Code != http.StatusNotFound {
							return fmt.Errorf("FIFO response status = %d, want 404", response.Code)
						}
						return nil
					})
				})
			}
		})
	}
}
