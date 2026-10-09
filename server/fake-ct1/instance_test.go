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
