package deployment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// ResetSuperAdminPassword runs the offline fleetd recovery command against the
// installed HA application profile. This preserves the generated environment,
// Compose project, and HA overlay selected by fleet-ha.
func ResetSuperAdminPassword(ctx context.Context, passwordStdin bool) error {
	config, err := loadNodeConfig(filepath.Join(configRoot, "node.env"))
	if err != nil {
		return err
	}
	if !config.isDatabaseNode() {
		return errors.New("HA password reset must run on ha-a or ha-b")
	}

	args, err := resetSuperAdminPasswordComposeArgsAt(installRoot, haGrafanaVolumeOwnershipMarker, passwordStdin)
	if err != nil {
		return err
	}
	return RunCompose(ctx, args)
}

func resetSuperAdminPasswordComposeArgsAt(root, ownershipMarker string, passwordStdin bool) ([]string, error) {
	command := []string{"--rm", "--no-deps", "-T", "fleet-api", "/app/fleetd", "admin", "reset-password"}
	if passwordStdin {
		command = append(command, "--password-stdin")
	}
	return fleetComposeArgsForInstalledProfileAt(root, ownershipMarker, "run", command...)
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
