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

func TestContainerConfigIdentityEnvironmentPrecedence(t *testing.T) {
	for _, test := range []struct {
		name, namespace, instance, server, wantNamespace, wantInstance string
	}{
		{name: "unset"},
		{name: "server", server: "controller", wantNamespace: "controller", wantInstance: "controller"},
		{name: "instance overrides server", instance: "legacy", server: "controller", wantNamespace: "legacy", wantInstance: "legacy"},
		{name: "explicit namespace", namespace: "inventory", instance: "legacy", server: "controller", wantNamespace: "inventory", wantInstance: "legacy"},
		{name: "namespace does not become instance", namespace: "inventory", server: "controller", wantNamespace: "inventory", wantInstance: "controller"},
		{name: "namespace without controller", namespace: "inventory", wantNamespace: "inventory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CT1_IDENTITY_NAMESPACE", test.namespace)
			t.Setenv("CT1_INSTANCE_ID", test.instance)
			t.Setenv("SERVER_ID", test.server)
			for _, key := range []string{"CT1_UNLOCATED_BOARDS", "CT1_OCCUPIED_BOARDS", "FAKE_MINER_RESPONSE_DELAY"} {
				t.Setenv(key, "")
			}
			cfg, err := containerConfigFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.IdentityNamespace != test.wantNamespace {
				t.Fatalf("namespace = %q, want %q", cfg.IdentityNamespace, test.wantNamespace)
			}
			if test.wantInstance != "" {
				if cfg.InstanceID != test.wantInstance {
					t.Fatalf("instance = %q, want %q", cfg.InstanceID, test.wantInstance)
				}
			} else if len(cfg.InstanceID) != 8 || cfg.InstanceID == test.namespace {
				t.Fatalf("unconfigured instance must keep its random prefix: %q", cfg.InstanceID)
			}
			if test.wantNamespace == "" {
				if got := NewContainerState(cfg).Modules[0].ModuleID; got != "T1-M01" {
					t.Fatalf("unconfigured module ID = %q", got)
				}
			}
		})
	}
}
