package miners

import (
	"encoding/json"
	"os"
	"testing"
)

type Manifest struct {
	Model           string            `json:"model"`
	Manufacturer    string            `json:"manufacturer"`
	Firmware        string            `json:"firmware"`
	FirmwareVersion string            `json:"firmware_version"`
	Plugin          string            `json:"plugin"`
	RecordedFrom    string            `json:"recorded_from"`
	RecordedAt      string            `json:"recorded_at"`
	Ports           map[string]int    `json:"ports"`
}

func LoadManifest(t testing.TB, path string) Manifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}
	return m
}
