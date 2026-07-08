package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/fatih/color"
	"github.com/urfave/cli/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// newFirmwareTestClient points a Client at srv and pre-seeds the session
// cookie so ensureSession never needs a fake auth endpoint. The trailing
// slash keeps the base URL at the server root, matching the documented
// direct fleet-api layout where firmware lives at /api/v1/firmware.
func newFirmwareTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	client, err := New(context.Background(), Options{Server: srv.URL + "/", Insecure: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	target, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	client.httpClient.Jar.SetCookies(target, []*http.Cookie{{Name: "fleet_session", Value: "test-session", Path: "/", Secure: true}})
	return client
}

func newFirmwareTestServer(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return newFirmwareTestClient(t, srv)
}

func writeFirmwareJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", contentTypeJSON)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode test response: %v", err)
	}
}

func serveFirmwareConfig(t *testing.T, mux *http.ServeMux, cfg firmwareConfig) {
	t.Helper()
	mux.HandleFunc("GET /api/v1/firmware/config", func(w http.ResponseWriter, _ *http.Request) {
		writeFirmwareJSON(t, w, cfg)
	})
}

func serveFirmwareCheck(t *testing.T, mux *http.ServeMux, resp firmwareCheckResponse) {
	t.Helper()
	mux.HandleFunc("POST /api/v1/firmware/check", func(w http.ResponseWriter, _ *http.Request) {
		writeFirmwareJSON(t, w, resp)
	})
}

func forbidFirmwareEndpoint(t *testing.T, mux *http.ServeMux, pattern string) {
	t.Helper()
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected request", http.StatusTeapot)
	})
}

func writeTempFirmwareFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write temp firmware file: %v", err)
	}
	return path
}

func TestFileSHA256(t *testing.T) {
	content := []byte("fleet firmware payload")
	path := writeTempFirmwareFile(t, "fw.swu", content)

	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])

	got, err := fileSHA256(path)
	if err != nil {
		t.Fatalf("fileSHA256() error = %v", err)
	}
	if got != want {
		t.Fatalf("fileSHA256() = %q, want %q", got, want)
	}
}

func TestFirmwareURLUsesBasePath(t *testing.T) {
	tests := []struct {
		name     string
		server   string
		insecure bool
		want     string
	}{
		{
			name:   "bare host uses normalized api proxy path",
			server: "https://fleet.example.com",
			want:   "https://fleet.example.com/api-proxy/api/v1/firmware/config",
		},
		{
			name:   "explicit proxy path preserved",
			server: "https://fleet.example.com/api-proxy",
			want:   "https://fleet.example.com/api-proxy/api/v1/firmware/config",
		},
		{
			name:     "explicit trailing slash keeps direct api root",
			server:   "http://fleet.example.com:4000/",
			insecure: true,
			want:     "http://fleet.example.com:4000/api/v1/firmware/config",
		},
		{
			name:   "explicit path preserved",
			server: "https://fleet.example.com/custom",
			want:   "https://fleet.example.com/custom/api/v1/firmware/config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(context.Background(), Options{Server: tt.server, Insecure: tt.insecure})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if got := client.firmwareURL("config").String(); got != tt.want {
				t.Fatalf("firmwareURL(config) = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasAllowedExtension(t *testing.T) {
	allowed := []string{".swu", ".tar.gz", ".zip"}
	tests := []struct {
		name string
		file string
		want bool
	}{
		{name: "swu", file: "firmware.swu", want: true},
		{name: "uppercase", file: "FIRMWARE.SWU", want: true},
		{name: "multi dot suffix", file: "firmware.tar.gz", want: true},
		{name: "inner extension alone", file: "firmware.gz", want: false},
		{name: "unsupported", file: "firmware.txt", want: false},
		{name: "empty name", file: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasAllowedExtension(tt.file, allowed); got != tt.want {
				t.Fatalf("hasAllowedExtension(%q) = %v, want %v", tt.file, got, tt.want)
			}
		})
	}
}

func TestFirmwareCheckSendsSHA256(t *testing.T) {
	var gotContentType string
	var gotBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/firmware/check", func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read check request body: %v", err)
		}
		writeFirmwareJSON(t, w, firmwareCheckResponse{Exists: true, FirmwareFileID: "existing-id"})
	})
	client := newFirmwareTestServer(t, mux)

	digest := strings.Repeat("ab", 32)
	resp, err := client.FirmwareCheck(context.Background(), digest)
	if err != nil {
		t.Fatalf("FirmwareCheck() error = %v", err)
	}

	if gotContentType != contentTypeJSON {
		t.Errorf("check Content-Type = %q, want %q", gotContentType, contentTypeJSON)
	}
	wantBody := fmt.Sprintf(`{"sha256":%q}`, digest)
	if string(gotBody) != wantBody {
		t.Errorf("check body = %s, want %s", gotBody, wantBody)
	}
	if !resp.Exists || resp.FirmwareFileID != "existing-id" {
		t.Errorf("FirmwareCheck() = %+v, want exists with id existing-id", resp)
	}
}

func TestFirmwareConfigAppliesDefaults(t *testing.T) {
	t.Run("fallback sizes when omitted", func(t *testing.T) {
		mux := http.NewServeMux()
		serveFirmwareConfig(t, mux, firmwareConfig{AllowedExtensions: []string{".swu"}})
		client := newFirmwareTestServer(t, mux)

		cfg, err := client.FirmwareConfig(context.Background())
		if err != nil {
			t.Fatalf("FirmwareConfig() error = %v", err)
		}
		if cfg.ChunkSizeBytes != defaultFirmwareChunkSizeBytes {
			t.Errorf("ChunkSizeBytes = %d, want %d", cfg.ChunkSizeBytes, defaultFirmwareChunkSizeBytes)
		}
		if cfg.MaxFileSizeBytes != defaultFirmwareMaxFileSizeBytes {
			t.Errorf("MaxFileSizeBytes = %d, want %d", cfg.MaxFileSizeBytes, defaultFirmwareMaxFileSizeBytes)
		}
	})

	t.Run("missing allowed extensions rejected", func(t *testing.T) {
		mux := http.NewServeMux()
		serveFirmwareConfig(t, mux, firmwareConfig{})
		client := newFirmwareTestServer(t, mux)

		if _, err := client.FirmwareConfig(context.Background()); err == nil || !strings.Contains(err.Error(), "allowed extensions") {
			t.Fatalf("FirmwareConfig() error = %v, want allowed extensions error", err)
		}
	})
}

func TestFirmwareUploadReusesExistingFile(t *testing.T) {
	path := writeTempFirmwareFile(t, "fw.swu", []byte("firmware"))

	mux := http.NewServeMux()
	serveFirmwareConfig(t, mux, firmwareConfig{AllowedExtensions: []string{".swu"}, MaxFileSizeBytes: 1 << 20, ChunkSizeBytes: 1024})
	serveFirmwareCheck(t, mux, firmwareCheckResponse{Exists: true, FirmwareFileID: "existing-id"})
	forbidFirmwareEndpoint(t, mux, "POST /api/v1/firmware/upload")
	forbidFirmwareEndpoint(t, mux, "POST /api/v1/firmware/upload/chunked")
	client := newFirmwareTestServer(t, mux)

	result, reused, err := runFirmwareUpload(context.Background(), client, path, false, nil)
	if err != nil {
		t.Fatalf("runFirmwareUpload() error = %v", err)
	}
	if !reused {
		t.Error("runFirmwareUpload() reused = false, want true")
	}
	if result.FirmwareFileID != "existing-id" {
		t.Errorf("FirmwareFileID = %q, want %q", result.FirmwareFileID, "existing-id")
	}
}

func TestFirmwareUploadForceUploadsDespiteCheckHit(t *testing.T) {
	path := writeTempFirmwareFile(t, "fw.swu", []byte("firmware"))

	uploadHit := false
	mux := http.NewServeMux()
	serveFirmwareConfig(t, mux, firmwareConfig{AllowedExtensions: []string{".swu"}, MaxFileSizeBytes: 1 << 20, ChunkSizeBytes: 1024})
	serveFirmwareCheck(t, mux, firmwareCheckResponse{Exists: true, FirmwareFileID: "existing-id"})
	mux.HandleFunc("POST /api/v1/firmware/upload", func(w http.ResponseWriter, _ *http.Request) {
		uploadHit = true
		writeFirmwareJSON(t, w, firmwareUploadResponse{FirmwareFileID: "fresh-id"})
	})
	client := newFirmwareTestServer(t, mux)

	result, reused, err := runFirmwareUpload(context.Background(), client, path, true, nil)
	if err != nil {
		t.Fatalf("runFirmwareUpload() error = %v", err)
	}
	if !uploadHit {
		t.Error("direct upload endpoint was not called despite --force")
	}
	if reused {
		t.Error("runFirmwareUpload() reused = true, want false")
	}
	if result.FirmwareFileID != "fresh-id" {
		t.Errorf("FirmwareFileID = %q, want %q", result.FirmwareFileID, "fresh-id")
	}
}

func TestFirmwareUploadDirectUsesMultipart(t *testing.T) {
	content := make([]byte, 100)
	for i := range content {
		content[i] = byte(i)
	}

	tests := []struct {
		name      string
		chunkSize int64
	}{
		{name: "below chunk threshold", chunkSize: 1024},
		{name: "exactly at chunk threshold", chunkSize: int64(len(content))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempFirmwareFile(t, "test-firmware.swu", content)

			var gotFilename string
			var gotBytes []byte
			mux := http.NewServeMux()
			serveFirmwareConfig(t, mux, firmwareConfig{AllowedExtensions: []string{".swu"}, MaxFileSizeBytes: 1 << 20, ChunkSizeBytes: tt.chunkSize})
			serveFirmwareCheck(t, mux, firmwareCheckResponse{Exists: false})
			forbidFirmwareEndpoint(t, mux, "POST /api/v1/firmware/upload/chunked")
			mux.HandleFunc("POST /api/v1/firmware/upload", func(w http.ResponseWriter, r *http.Request) {
				file, header, err := r.FormFile("file")
				if err != nil {
					t.Errorf("multipart form field %q missing: %v", "file", err)
					http.Error(w, "bad form", http.StatusBadRequest)
					return
				}
				defer func() { _ = file.Close() }()
				gotFilename = header.Filename
				gotBytes, err = io.ReadAll(file)
				if err != nil {
					t.Errorf("read multipart file: %v", err)
				}
				writeFirmwareJSON(t, w, firmwareUploadResponse{FirmwareFileID: "direct-id"})
			})
			client := newFirmwareTestServer(t, mux)

			result, reused, err := runFirmwareUpload(context.Background(), client, path, false, nil)
			if err != nil {
				t.Fatalf("runFirmwareUpload() error = %v", err)
			}
			if reused {
				t.Error("runFirmwareUpload() reused = true, want false")
			}
			if result.FirmwareFileID != "direct-id" {
				t.Errorf("FirmwareFileID = %q, want %q", result.FirmwareFileID, "direct-id")
			}
			if gotFilename != "test-firmware.swu" {
				t.Errorf("multipart filename = %q, want %q", gotFilename, "test-firmware.swu")
			}
			if !bytes.Equal(gotBytes, content) {
				t.Errorf("uploaded bytes do not match the source file (got %d bytes, want %d)", len(gotBytes), len(content))
			}
		})
	}
}

func TestFirmwareUploadChunkedSequence(t *testing.T) {
	content := []byte("abcdefghijklmnopqrst") // 20 bytes -> chunks of 8, 8, 4
	path := writeTempFirmwareFile(t, "big-firmware.swu", content)

	var initiateBody firmwareInitiateRequest
	var gotRanges []string
	var gotChunkTypes []string
	var reassembled []byte
	mux := http.NewServeMux()
	serveFirmwareConfig(t, mux, firmwareConfig{AllowedExtensions: []string{".swu"}, MaxFileSizeBytes: 1 << 20, ChunkSizeBytes: 8})
	serveFirmwareCheck(t, mux, firmwareCheckResponse{Exists: false})
	forbidFirmwareEndpoint(t, mux, "POST /api/v1/firmware/upload")
	mux.HandleFunc("POST /api/v1/firmware/upload/chunked", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&initiateBody); err != nil {
			t.Errorf("decode initiate body: %v", err)
		}
		writeFirmwareJSON(t, w, firmwareInitiateResponse{UploadID: "u1"})
	})
	mux.HandleFunc("PUT /api/v1/firmware/upload/chunked/{uploadID}", func(w http.ResponseWriter, r *http.Request) {
		if got := r.PathValue("uploadID"); got != "u1" {
			t.Errorf("chunk upload id = %q, want %q", got, "u1")
		}
		gotRanges = append(gotRanges, r.Header.Get("Content-Range"))
		gotChunkTypes = append(gotChunkTypes, r.Header.Get("Content-Type"))
		chunk, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read chunk body: %v", err)
		}
		reassembled = append(reassembled, chunk...)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /api/v1/firmware/upload/chunked/{uploadID}/complete", func(w http.ResponseWriter, r *http.Request) {
		if got := r.PathValue("uploadID"); got != "u1" {
			t.Errorf("complete upload id = %q, want %q", got, "u1")
		}
		writeFirmwareJSON(t, w, firmwareUploadResponse{FirmwareFileID: "chunked-id"})
	})
	client := newFirmwareTestServer(t, mux)

	result, reused, err := runFirmwareUpload(context.Background(), client, path, false, nil)
	if err != nil {
		t.Fatalf("runFirmwareUpload() error = %v", err)
	}
	if reused {
		t.Error("runFirmwareUpload() reused = true, want false")
	}
	if result.FirmwareFileID != "chunked-id" {
		t.Errorf("FirmwareFileID = %q, want %q", result.FirmwareFileID, "chunked-id")
	}
	if initiateBody.Filename != "big-firmware.swu" || initiateBody.FileSize != int64(len(content)) {
		t.Errorf("initiate body = %+v, want filename big-firmware.swu and file_size %d", initiateBody, len(content))
	}
	wantRanges := []string{"bytes 0-7/20", "bytes 8-15/20", "bytes 16-19/20"}
	if len(gotRanges) != len(wantRanges) {
		t.Fatalf("chunk count = %d (%v), want %d", len(gotRanges), gotRanges, len(wantRanges))
	}
	for i, want := range wantRanges {
		if gotRanges[i] != want {
			t.Errorf("chunk %d Content-Range = %q, want %q", i, gotRanges[i], want)
		}
		if gotChunkTypes[i] != contentTypeOctetStream {
			t.Errorf("chunk %d Content-Type = %q, want %q", i, gotChunkTypes[i], contentTypeOctetStream)
		}
	}
	if !bytes.Equal(reassembled, content) {
		t.Errorf("reassembled chunks do not match the source file (got %q, want %q)", reassembled, content)
	}
}

func TestFirmwareUploadValidationRejectsLocally(t *testing.T) {
	tests := []struct {
		name    string
		path    func(t *testing.T) string
		wantErr string
	}{
		{
			name:    "unsupported extension",
			path:    func(t *testing.T) string { return writeTempFirmwareFile(t, "fw.txt", []byte("x")) },
			wantErr: "unsupported firmware file type",
		},
		{
			name:    "empty file",
			path:    func(t *testing.T) string { return writeTempFirmwareFile(t, "fw.swu", nil) },
			wantErr: "is empty",
		},
		{
			name:    "oversized file",
			path:    func(t *testing.T) string { return writeTempFirmwareFile(t, "fw.swu", []byte("12345678901")) },
			wantErr: "exceeding the maximum",
		},
		{
			name:    "directory",
			path:    func(t *testing.T) string { return t.TempDir() },
			wantErr: "is a directory",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			serveFirmwareConfig(t, mux, firmwareConfig{AllowedExtensions: []string{".swu"}, MaxFileSizeBytes: 10, ChunkSizeBytes: 8})
			forbidFirmwareEndpoint(t, mux, "POST /api/v1/firmware/check")
			forbidFirmwareEndpoint(t, mux, "POST /api/v1/firmware/upload")
			forbidFirmwareEndpoint(t, mux, "POST /api/v1/firmware/upload/chunked")
			client := newFirmwareTestServer(t, mux)

			_, _, err := runFirmwareUpload(context.Background(), client, tt.path(t), false, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("runFirmwareUpload() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestFirmwareListDecodesFiles(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/firmware/files", func(w http.ResponseWriter, _ *http.Request) {
		writeFirmwareJSON(t, w, firmwareListResponse{Files: []firmwareFileInfo{
			{ID: "id-1", Filename: "fw.swu", Size: 42, UploadedAt: "2026-06-10T14:30:45Z"},
		}})
	})
	client := newFirmwareTestServer(t, mux)

	resp, err := client.FirmwareList(context.Background())
	if err != nil {
		t.Fatalf("FirmwareList() error = %v", err)
	}
	if len(resp.Files) != 1 {
		t.Fatalf("FirmwareList() returned %d files, want 1", len(resp.Files))
	}
	file := resp.Files[0]
	if file.ID != "id-1" || file.Filename != "fw.swu" || file.Size != 42 || file.UploadedAt != "2026-06-10T14:30:45Z" {
		t.Errorf("FirmwareList() file = %+v", file)
	}
}

func TestFirmwareDeleteHitsFileEndpoint(t *testing.T) {
	tests := []struct {
		name   string
		fileID string
	}{
		{name: "uuid id", fileID: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "id with space", fileID: "abc def"},
		{name: "id with percent", fileID: "abc%def"},
		{name: "id with slash", fileID: "abc/def"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotID, gotEscapedPath string
			mux := http.NewServeMux()
			mux.HandleFunc("DELETE /api/v1/firmware/files/", func(w http.ResponseWriter, r *http.Request) {
				gotEscapedPath = r.URL.EscapedPath()
				escapedID := strings.TrimPrefix(gotEscapedPath, "/api/v1/firmware/files/")
				var err error
				gotID, err = url.PathUnescape(escapedID)
				if err != nil {
					t.Errorf("unescape file id %q: %v", escapedID, err)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			client := newFirmwareTestServer(t, mux)

			if err := client.FirmwareDelete(context.Background(), tt.fileID); err != nil {
				t.Fatalf("FirmwareDelete() error = %v", err)
			}
			if gotID != tt.fileID {
				t.Errorf("deleted file id = %q, want %q", gotID, tt.fileID)
			}
			wantPath := "/api/v1/firmware/files/" + url.PathEscape(tt.fileID)
			if gotEscapedPath != wantPath {
				t.Errorf("request path = %q, want %q", gotEscapedPath, wantPath)
			}
		})
	}
}

func TestFirmwareDeleteAllReturnsCount(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/firmware/files", func(w http.ResponseWriter, _ *http.Request) {
		writeFirmwareJSON(t, w, firmwareDeleteAllResponse{DeletedCount: 3})
	})
	client := newFirmwareTestServer(t, mux)

	resp, err := client.FirmwareDeleteAll(context.Background())
	if err != nil {
		t.Fatalf("FirmwareDeleteAll() error = %v", err)
	}
	if resp.DeletedCount != 3 {
		t.Errorf("DeletedCount = %d, want 3", resp.DeletedCount)
	}
}

func TestConfirmFirmwareDeleteAllPrompt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "y confirms", input: "y\n"},
		{name: "yes confirms", input: "yes\n"},
		{name: "default cancels", input: "\n", wantErr: "cancelled"},
		{name: "n cancels", input: "n\n", wantErr: "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var prompt bytes.Buffer
			err := confirmFirmwareDeleteAll(false, strings.NewReader(tt.input), &prompt)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("confirmFirmwareDeleteAll() error = %v, want nil", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("confirmFirmwareDeleteAll() error = %v, want containing %q", err, tt.wantErr)
			}
			if !strings.Contains(prompt.String(), "[y/N]") {
				t.Fatalf("prompt = %q, want y/N prompt", prompt.String())
			}
		})
	}
}

func TestFirmwareDeleteAllCommandCancelsBeforeRequest(t *testing.T) {
	pinFleetAuthEnv(t, map[string]string{envFleetPassword: "proto"})

	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected request", http.StatusTeapot)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	withStdin(t, "n\n", func() {
		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--username", "admin",
			"firmware", "delete-all",
		})
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Fatalf("firmware delete-all error = %v, want cancellation", err)
		}
	})
	if requestCount != 0 {
		t.Fatalf("request count = %d, want 0", requestCount)
	}
}

func TestFirmwareDeleteAllCommandDeletesAfterYes(t *testing.T) {
	pinFleetAuthEnv(t, map[string]string{envFleetPassword: "proto"})
	oldNoColor := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = oldNoColor })

	tests := []struct {
		name string
		flag string
	}{
		{name: "long flag", flag: "--yes"},
		{name: "short flag", flag: "-y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleteCount := 0
			mux := http.NewServeMux()
			mux.HandleFunc("POST /auth.v1.AuthService/Authenticate", func(w http.ResponseWriter, _ *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "fleet_session", Value: "test-session", Path: "/", Secure: true})
				w.Header().Set("Content-Type", contentTypeJSON)
				_, _ = w.Write([]byte(`{}`))
			})
			mux.HandleFunc("DELETE /api/v1/firmware/files", func(w http.ResponseWriter, _ *http.Request) {
				deleteCount++
				writeFirmwareJSON(t, w, firmwareDeleteAllResponse{DeletedCount: 3})
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			output := captureStdout(t, func() {
				err := newRootCommand().Run(context.Background(), []string{
					"fleetcli", "--server", srv.URL + "/", "--username", "admin",
					"firmware", "delete-all", tt.flag,
				})
				if err != nil {
					t.Fatalf("firmware delete-all error = %v", err)
				}
			})

			if deleteCount != 1 {
				t.Fatalf("delete count = %d, want 1", deleteCount)
			}
			var decoded firmwareDeleteAllResponse
			if err := json.Unmarshal([]byte(output), &decoded); err != nil {
				t.Fatalf("output is not JSON: %s", output)
			}
			if decoded.DeletedCount != 3 {
				t.Fatalf("deleted_count = %d, want 3", decoded.DeletedCount)
			}
		})
	}
}

func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()

	oldStdin := os.Stdin
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	if _, err := write.WriteString(input); err != nil {
		t.Fatalf("write stdin pipe: %v", err)
	}
	if err := write.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = read
	defer func() {
		os.Stdin = oldStdin
		_ = read.Close()
	}()

	fn()
}

func buildFirmwareDeployRequestFromArgs(t *testing.T, args ...string) (*minercommandv1.FirmwareUpdateRequest, error) {
	t.Helper()

	var req *minercommandv1.FirmwareUpdateRequest
	var buildErr error
	cmd := firmwareDeployCommand()
	cmd.Action = func(ctx context.Context, cmd *cli.Command) error {
		req, buildErr = buildFirmwareDeployRequest(ctx, cmd, nil)
		return nil
	}
	if err := cmd.Run(context.Background(), append([]string{"deploy"}, args...)); err != nil {
		return nil, fmt.Errorf("run firmware deploy command: %w", err)
	}
	return req, buildErr
}

func TestFirmwareDeployBuildsExplicitDeviceRequest(t *testing.T) {
	req, err := buildFirmwareDeployRequestFromArgs(t,
		"--firmware-file-id", " firmware-1 ",
		"--device", "device-b",
		"--device", " device-a ",
		"--device", "device-b",
	)
	if err != nil {
		t.Fatalf("buildFirmwareDeployRequest() error = %v", err)
	}

	want := &minercommandv1.FirmwareUpdateRequest{
		FirmwareFileId: "firmware-1",
		DeviceSelector: &minercommandv1.DeviceSelector{
			SelectionType: &minercommandv1.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: []string{"device-a", "device-b"},
				},
			},
		},
	}
	if !proto.Equal(req, want) {
		t.Fatalf("request = %v, want %v", req, want)
	}
}

func TestFirmwareDeployRequiresFirmwareFileIDAndDevice(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing firmware file id", args: []string{"--device", "device-a"}, wantErr: "firmware-file-id"},
		{name: "missing selector", args: []string{"--firmware-file-id", "firmware-1"}, wantErr: "one of --device"},
		{name: "blank device", args: []string{"--firmware-file-id", "firmware-1", "--device", "  "}, wantErr: "one of --device"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildFirmwareDeployRequestFromArgs(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("firmware deploy %v error = %v, want containing %q", tt.args, err, tt.wantErr)
			}
		})
	}
}

func TestFirmwareDeployRejectsAllDevicesBeforeRequest(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected request", http.StatusTeapot)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// --all-devices is intentionally not a deploy flag, so it must be rejected
	// during parsing before any RPC is issued.
	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"firmware", "deploy", "--firmware-file-id", "firmware-1", "--device", "device-a", "--all-devices",
	})
	if err == nil {
		t.Fatal("firmware deploy accepted --all-devices")
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want 0", requestCount)
	}
}

func TestFirmwareDeployCallsFirmwareUpdateAndPrintsBatch(t *testing.T) {
	pinFleetAuthEnv(t, nil)
	oldNoColor := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = oldNoColor })

	var gotAuth string
	var gotReq minercommandv1.FirmwareUpdateRequest
	mux := http.NewServeMux()
	mux.HandleFunc("POST /minercommand.v1.MinerCommandService/FirmwareUpdate", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read firmware deploy request: %v", err)
		}
		if err := protojson.Unmarshal(body, &gotReq); err != nil {
			t.Errorf("decode firmware deploy request: %v", err)
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{"batch_identifier":"batch-1"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	output := captureStdout(t, func() {
		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			"firmware", "deploy", "--firmware-file-id", "firmware-1", "--device", "device-a", "--device", "device-b",
		})
		if err != nil {
			t.Fatalf("firmware deploy error = %v", err)
		}
	})

	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}
	wantReq := &minercommandv1.FirmwareUpdateRequest{
		FirmwareFileId: "firmware-1",
		DeviceSelector: &minercommandv1.DeviceSelector{
			SelectionType: &minercommandv1.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: []string{"device-a", "device-b"},
				},
			},
		},
	}
	if !proto.Equal(&gotReq, wantReq) {
		t.Fatalf("request = %v, want %v", &gotReq, wantReq)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("output is not JSON: %s", output)
	}
	if decoded["batch_identifier"] != "batch-1" {
		t.Fatalf("batch_identifier = %v, want batch-1", decoded["batch_identifier"])
	}
}

func TestFirmwareDeployResolvesGroupToDevices(t *testing.T) {
	pinFleetAuthEnv(t, nil)
	oldNoColor := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = oldNoColor })

	var gotReq minercommandv1.FirmwareUpdateRequest
	mux := http.NewServeMux()
	mux.HandleFunc("POST /collection.v1.DeviceCollectionService/ListCollections", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{"collections":[{"id":"7","label":"group-a"}]}`))
	})
	mux.HandleFunc("POST /collection.v1.DeviceCollectionService/GetCollection", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{"collection":{"id":"7","type":"COLLECTION_TYPE_GROUP","label":"group-a"}}`))
	})
	mux.HandleFunc("POST /collection.v1.DeviceCollectionService/ListCollectionMembers", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{"members":[{"device_identifier":"device-b"},{"device_identifier":"device-a"}]}`))
	})
	mux.HandleFunc("POST /minercommand.v1.MinerCommandService/FirmwareUpdate", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read firmware deploy request: %v", err)
		}
		if err := protojson.Unmarshal(body, &gotReq); err != nil {
			t.Errorf("decode firmware deploy request: %v", err)
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{"batch_identifier":"batch-1"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	_ = captureStdout(t, func() {
		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			"firmware", "deploy", "--firmware-file-id", "firmware-1", "--group", "group-a",
		})
		if err != nil {
			t.Fatalf("firmware deploy error = %v", err)
		}
	})

	wantReq := &minercommandv1.FirmwareUpdateRequest{
		FirmwareFileId: "firmware-1",
		DeviceSelector: &minercommandv1.DeviceSelector{
			SelectionType: &minercommandv1.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: []string{"device-a", "device-b"},
				},
			},
		},
	}
	if !proto.Equal(&gotReq, wantReq) {
		t.Fatalf("request = %v, want %v", &gotReq, wantReq)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = write
	defer func() { os.Stdout = oldStdout }()

	fn()

	if err := write.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	output, err := io.ReadAll(read)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	if err := read.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return string(output)
}

func TestFirmwareAPIErrorIncludesMethodLabel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/firmware/config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentTypeJSON)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"authentication required"}`)
	})
	client := newFirmwareTestServer(t, mux)

	_, err := client.FirmwareConfig(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("FirmwareConfig() error = %v, want *APIError", err)
	}
	if apiErr.Method != "GET /api/v1/firmware/config" {
		t.Errorf("APIError.Method = %q, want %q", apiErr.Method, "GET /api/v1/firmware/config")
	}
	if !strings.Contains(apiErr.Status, "401") {
		t.Errorf("APIError.Status = %q, want containing 401", apiErr.Status)
	}
	if !strings.Contains(string(apiErr.Body), "authentication required") {
		t.Errorf("APIError.Body = %q, want containing the server error message", apiErr.Body)
	}
}

func TestFirmwareRequiresSessionCredentials(t *testing.T) {
	client, err := New(context.Background(), Options{Server: "http://127.0.0.1:1/", Insecure: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.FirmwareList(context.Background())
	if err == nil {
		t.Fatal("FirmwareList() error = nil, want missing credentials error")
	}
	for _, want := range []string{"--username", envFleetPassword, "--password-stdin"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("FirmwareList() error = %v, want containing %q", err, want)
		}
	}
}

func TestFirmwareSessionFailureWithCredentialsKeepsRealCause(t *testing.T) {
	// Credentials are present but nothing listens on the target port, so the
	// error must surface the connection failure rather than blaming missing
	// credentials.
	client, err := New(context.Background(), Options{Server: "http://127.0.0.1:1/", Insecure: true, Username: "admin", Password: "proto"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.FirmwareList(context.Background())
	if err == nil {
		t.Fatal("FirmwareList() error = nil, want connection error")
	}
	if !strings.Contains(err.Error(), "establish firmware session") {
		t.Errorf("FirmwareList() error = %v, want containing %q", err, "establish firmware session")
	}
	if strings.Contains(err.Error(), "password from") {
		t.Errorf("FirmwareList() error = %v, must not suggest missing credentials when they were provided", err)
	}
}

func TestFirmwareSingleArgValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no arguments", args: []string{"check"}},
		{name: "too many arguments", args: []string{"check", "a.swu", "b.swu"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := firmwareCheckCommand().Run(context.Background(), tt.args)
			if err == nil || !strings.Contains(err.Error(), "expected exactly one argument") {
				t.Fatalf("check %v error = %v, want single-argument usage error", tt.args, err)
			}
		})
	}
}
