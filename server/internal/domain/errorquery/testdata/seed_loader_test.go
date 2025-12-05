package testdata

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadSeedFile(t *testing.T) {
	// Get the directory of this test file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get test file path")
	}
	testdataDir := filepath.Dir(filename)
	seedFile := filepath.Join(testdataDir, "seed_errors.yaml")

	seedData, err := LoadSeedFile(seedFile)
	if err != nil {
		t.Fatalf("failed to load seed file: %v", err)
	}

	// Verify we have 12 devices
	if len(seedData) != 12 {
		t.Errorf("expected 12 devices, got %d", len(seedData))
	}

	// Verify miners 1-5 have no errors
	for i := 0; i < 5; i++ {
		if seedData[i].DeviceID != int64(i+1) {
			t.Errorf("device %d: expected device_id %d, got %d", i, i+1, seedData[i].DeviceID)
		}
		if len(seedData[i].Errors) != 0 {
			t.Errorf("device %d: expected 0 errors, got %d", seedData[i].DeviceID, len(seedData[i].Errors))
		}
	}

	// Verify miners 6-12 have hashboard over temperature errors
	for i := 5; i < 12; i++ {
		if seedData[i].DeviceID != int64(i+1) {
			t.Errorf("device %d: expected device_id %d, got %d", i, i+1, seedData[i].DeviceID)
		}
		if len(seedData[i].Errors) != 1 {
			t.Errorf("device %d: expected 1 error, got %d", seedData[i].DeviceID, len(seedData[i].Errors))
			continue
		}
		// Check it's the right error type (MinerError enum value)
		if seedData[i].Errors[0].MinerError.String() != "MINER_ERROR_HASHBOARD_OVER_TEMPERATURE" {
			t.Errorf("device %d: expected MINER_ERROR_HASHBOARD_OVER_TEMPERATURE, got %s",
				seedData[i].DeviceID, seedData[i].Errors[0].MinerError.String())
		}
	}
}
