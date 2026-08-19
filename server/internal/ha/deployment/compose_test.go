package deployment

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInstalledFleetComposeArgsSupportLegacyAndGrafanaProfiles(t *testing.T) {
	for _, test := range []struct {
		name        string
		ownsGrafana bool
	}{
		{name: "legacy"},
		{name: "Grafana", ownsGrafana: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			alertsPath := filepath.Join(root, "docker-compose.alerts.yaml")
			if err := os.WriteFile(alertsPath, []byte("services: {}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ownershipMarker := filepath.Join(root, "grafana-volume-owned")
			if test.ownsGrafana {
				if err := os.WriteFile(ownershipMarker, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			args, err := fleetComposeArgsForInstalledProfileAt(root, ownershipMarker, "down")
			if err != nil {
				t.Fatal(err)
			}
			hasAlerts := slices.Contains(args, alertsPath)
			if hasAlerts != test.ownsGrafana {
				t.Fatalf("alerts Compose included = %t, want %t; args = %q", hasAlerts, test.ownsGrafana, args)
			}
		})
	}
}

func TestResetSuperAdminPasswordComposeArgsUseInstalledHAProfile(t *testing.T) {
	root := t.TempDir()
	ownershipMarker := filepath.Join(root, "grafana-volume-owned")

	args, err := resetSuperAdminPasswordComposeArgsAt(root, ownershipMarker, true)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"--project-name deployment",
		"--env-file /etc/proto-fleet/ha/base.env",
		"--env-file /etc/proto-fleet/ha/fleet.env",
		"--env-file /etc/proto-fleet/ha/node.env",
		"--file " + filepath.Join(root, "docker-compose.yaml"),
		"--file " + filepath.Join(root, "ha/fleet-compose.yaml"),
		"run --rm -T fleet-api /app/fleetd admin reset-password --password-stdin",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("reset args %q do not contain %q", args, expected)
		}
	}
	if strings.Contains(joined, "docker-compose.alerts.yaml") {
		t.Fatalf("legacy HA reset unexpectedly included alerts profile: %q", args)
	}

	if err := os.WriteFile(ownershipMarker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	args, err = resetSuperAdminPasswordComposeArgsAt(root, ownershipMarker, false)
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, "--file "+filepath.Join(root, "docker-compose.alerts.yaml")) {
		t.Fatalf("Grafana-owning HA reset omitted alerts profile: %q", args)
	}
	if strings.Contains(joined, "--password-stdin") {
		t.Fatalf("generated-password HA reset unexpectedly enabled stdin mode: %q", args)
	}
}

func TestRunComposeRejectsParentOverrides(t *testing.T) {
	for _, key := range []string{"AUTH_CLIENT_SECRET_KEY", "COMPOSE_PROJECT_NAME", "DB_DSN", "HA_NODE_IP", "GRAFANA_DB_PASSWORD", "FLEET_ALERTS_GRAFANA_URL"} {
		t.Run(key, func(t *testing.T) {
			// Arrange
			const value = "must-not-appear-in-errors"
			t.Setenv(key, value)

			// Act
			err := RunCompose(context.Background(), []string{"config"})

			// Assert
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("RunCompose() error = %v", err)
			}
			if strings.Contains(err.Error(), value) {
				t.Fatal("RunCompose() exposed the parent value")
			}
		})
	}
}
