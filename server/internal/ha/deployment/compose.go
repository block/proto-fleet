package deployment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
)

const localDockerHost = "unix:///var/run/docker.sock"

func fleetApplicationComposeArgs(operation string, flags ...string) []string {
	return fleetApplicationComposeArgsAt(installRoot, operation, flags...)
}

func fleetApplicationComposeArgsAt(root, operation string, flags ...string) []string {
	args := slices.Clone(flags)
	args = append(args, "fleet-api", "fleet-client", "grafana")
	return fleetComposeArgsAt(root, operation, args...)
}

func fleetComposeArgsForInstalledProfile(operation string, flags ...string) ([]string, error) {
	return fleetComposeArgsForInstalledProfileAt(installRoot, haGrafanaVolumeOwnershipMarker, operation, flags...)
}

func fleetComposeArgsForInstalledProfileAt(root, ownershipMarker, operation string, flags ...string) ([]string, error) {
	if _, err := os.Stat(ownershipMarker); err == nil {
		return fleetComposeArgsAtProfile(root, true, operation, flags...), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect installed HA Grafana ownership marker: %w", err)
	}
	return fleetComposeArgsAtProfile(root, false, operation, flags...), nil
}

// RunCompose prevents parent variables from overriding generated HA identity and secrets.
func RunCompose(ctx context.Context, args []string) error {
	protected := []string{
		"AUTH_CLIENT_SECRET_KEY",
		"COMPOSE_PROJECT_NAME",
		"DB_DSN",
		"ENCRYPT_SERVICE_MASTER_KEY",
		"FLEET_ALERTS_ENABLED",
		"FLEET_ALERTS_GRAFANA_TOKEN",
		"FLEET_ALERTS_GRAFANA_URL",
		"FLEET_ALERTS_WEBHOOK_TOKEN",
		"GRAFANA_ADMIN_PASSWORD",
		"GRAFANA_DB_PASSWORD",
		"GRAFANA_SECRET_KEY",
	}
	for key := range allowedEnvKeys {
		protected = append(protected, key)
	}
	slices.Sort(protected)
	for _, key := range protected {
		if _, ok := os.LookupEnv(key); ok {
			return fmt.Errorf("parent environment must not set %s; unset it before running Fleet", key)
		}
	}

	commandArgs := append([]string{"--host", localDockerHost, "compose"}, args...)
	command := exec.CommandContext(ctx, "docker", commandArgs...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run Docker Compose: %w", err)
	}
	return nil
}
