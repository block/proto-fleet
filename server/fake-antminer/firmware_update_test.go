package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func firmwareUploadRequest(t *testing.T, filename string) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create firmware form file: %v", err)
	}
	if _, err := part.Write([]byte("fake firmware")); err != nil {
		t.Fatalf("write firmware form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/cgi-bin/upgrade.cgi", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestFirmwareVersionFromFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
		wantOK   bool
	}{
		{name: "release filename", filename: "miner-image-release-c3-p1-1.3.5.swu", want: "1.3.5", wantOK: true},
		{name: "v prefix", filename: "antminer-s19-v2.1.0.tar.gz", want: "2.1.0", wantOK: true},
		{name: "no version", filename: "antminer-firmware.tar.gz", wantOK: false},
		{name: "partial version", filename: "antminer-2.1.tar.gz", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := firmwareVersionFromFilename(tt.filename)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("firmwareVersionFromFilename(%q) = %q, %v; want %q, %v", tt.filename, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestFirmwareUploadPromotesFilenameVersionOnReboot(t *testing.T) {
	state := &MinerState{FirmwareVersion: "2.0.0"}
	uploadRecorder := httptest.NewRecorder()

	createUpgradeHandler(state).ServeHTTP(uploadRecorder, firmwareUploadRequest(t, "antminer-s19-v2.1.0.tar.gz"))

	if uploadRecorder.Code != http.StatusOK {
		t.Fatalf("expected upload status %d, got %d", http.StatusOK, uploadRecorder.Code)
	}
	if state.FirmwareVersion != "2.0.0" {
		t.Fatalf("expected current firmware to remain 2.0.0 before reboot, got %q", state.FirmwareVersion)
	}
	if state.PendingFirmwareVersion != "2.1.0" {
		t.Fatalf("expected pending firmware 2.1.0, got %q", state.PendingFirmwareVersion)
	}

	rebootRecorder := httptest.NewRecorder()
	createRebootHandler(state).ServeHTTP(
		rebootRecorder,
		httptest.NewRequest(http.MethodPost, "/cgi-bin/reboot.cgi", nil),
	)

	if rebootRecorder.Code != http.StatusOK {
		t.Fatalf("expected reboot status %d, got %d", http.StatusOK, rebootRecorder.Code)
	}
	if state.FirmwareVersion != "2.1.0" {
		t.Fatalf("expected current firmware 2.1.0 after reboot, got %q", state.FirmwareVersion)
	}
	if state.PendingFirmwareVersion != "" {
		t.Fatalf("expected pending firmware to be cleared, got %q", state.PendingFirmwareVersion)
	}
	if got := generateVersionResponse(state).Version[0].BMMiner; got != "2.1.0" {
		t.Fatalf("expected RPC firmware version 2.1.0, got %q", got)
	}
}

func TestFirmwareUploadWithoutFilenameVersionPreservesCurrentVersion(t *testing.T) {
	state := &MinerState{FirmwareVersion: "2.0.0"}
	uploadRecorder := httptest.NewRecorder()

	createUpgradeHandler(state).ServeHTTP(uploadRecorder, firmwareUploadRequest(t, "antminer-firmware.tar.gz"))

	if uploadRecorder.Code != http.StatusOK {
		t.Fatalf("expected upload status %d, got %d", http.StatusOK, uploadRecorder.Code)
	}
	if state.PendingFirmwareVersion != "" {
		t.Fatalf("expected no pending firmware version, got %q", state.PendingFirmwareVersion)
	}

	rebootRecorder := httptest.NewRecorder()
	createRebootHandler(state).ServeHTTP(
		rebootRecorder,
		httptest.NewRequest(http.MethodPost, "/cgi-bin/reboot.cgi", nil),
	)

	if state.FirmwareVersion != "2.0.0" {
		t.Fatalf("expected current firmware to remain 2.0.0, got %q", state.FirmwareVersion)
	}
}
