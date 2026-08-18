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
		name       string
		withAlerts bool
	}{
		{name: "legacy"},
		{name: "Grafana", withAlerts: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			alertsPath := filepath.Join(root, "docker-compose.alerts.yaml")
			if test.withAlerts {
				if err := os.WriteFile(alertsPath, []byte("services: {}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			args, err := fleetComposeArgsForInstalledProfileAt(root, "down")
			if err != nil {
				t.Fatal(err)
			}
			hasAlerts := slices.Contains(args, alertsPath)
			if hasAlerts != test.withAlerts {
				t.Fatalf("alerts Compose included = %t, want %t; args = %q", hasAlerts, test.withAlerts, args)
			}
		})
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
