package main

import "testing"

func TestConfiguredControllerKeepsBoardIdentityAcrossRestarts(t *testing.T) {
	t.Setenv("SERVER_ID", "ct1-sim-001")
	t.Setenv("CT1_INSTANCE_ID", "")
	first := simulatorInstanceID()
	second := simulatorInstanceID()
	if first != second {
		t.Fatalf("same controller changed board identity prefix: %q -> %q", first, second)
	}
}

func TestLegacyInstanceIDCanBePreserved(t *testing.T) {
	t.Setenv("SERVER_ID", "ct1-sim-001")
	t.Setenv("CT1_INSTANCE_ID", "legacy-id")
	if got := simulatorInstanceID(); got != "legacy-id" {
		t.Fatalf("got %q, want legacy-id", got)
	}
}

func TestDifferentControllersHaveDifferentBoardMACs(t *testing.T) {
	// Arrange
	cfg := testConfig()
	cfg.InstanceID = "controller-1"

	// Act
	first := NewContainerState(cfg)
	cfg.InstanceID = "controller-2"
	second := NewContainerState(cfg)

	// Assert
	for _, b := range first.eachBoard() {
		if second.findBoard(b.MAC) != nil {
			t.Fatalf("controllers share hashboard MAC %s", b.MAC)
		}
	}
}
