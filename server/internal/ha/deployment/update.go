package deployment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/block/proto-fleet/server/internal/ha"
)

func RequirePassive(ctx context.Context, envPath string) error {
	report, err := Status(ctx, envPath, true)
	if err != nil {
		return err
	}
	if report.Runtime.Observation != ha.ObservationCurrent || report.Runtime.Role != ha.RolePassive {
		return fmt.Errorf("HA application update requires a healthy passive node; local role is %s and observation is %s", report.Runtime.Role, report.Runtime.Observation)
	}
	if report.Control == nil || !report.Control.FailoverReady {
		return errors.New("HA application update requires full failover readiness")
	}
	return nil
}

// PrepareApplicationUpdate builds only the Fleet API and client from a verified release.
func PrepareApplicationUpdate(ctx context.Context, root string) error {
	deps := defaultInstallDependencies()
	if err := validateRelease(ctx, root, deps); err != nil {
		return err
	}
	// Prune before building so repeated updates cannot exhaust a small HA host.
	for _, args := range [][]string{{"image", "prune", "-f"}, {"builder", "prune", "-f"}} {
		if output, err := runCommand(ctx, "docker", args...); err != nil {
			return fmt.Errorf("clean previous HA application build artifacts: %s", commandError(output, err))
		}
	}
	if output, err := runCommand(ctx, "docker", "load", "--input", filepath.Join(root, "images", "timescaledb.tar.gz")); err != nil {
		return fmt.Errorf("load HA database image for future restarts: %s", commandError(output, err))
	}
	nginx, err := os.ReadFile(filepath.Join(root, "client", "nginx.https.conf"))
	if err != nil {
		return fmt.Errorf("read HA nginx configuration: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, "client", "nginx.conf"), nginx, 0o644); err != nil {
		return fmt.Errorf("prepare HA nginx configuration: %w", err)
	}
	if err := RunCompose(ctx, fleetComposeArgsAt(root, "build", "fleet-api", "fleet-client")); err != nil {
		return fmt.Errorf("build HA application update: %w", err)
	}
	return nil
}

// StopApplication stops only Fleet containers; the HA substrate keeps running.
func StopApplication(ctx context.Context, root string) error {
	if err := RequirePassive(ctx, filepath.Join(configRoot, "node.env")); err != nil {
		return err
	}
	if err := RunCompose(ctx, fleetComposeArgsAt(root, "stop", "fleet-api", "fleet-client")); err != nil {
		return fmt.Errorf("stop HA application: %w", err)
	}
	return nil
}

// StartApplication starts the target release and proves it serves its observed HA role.
func StartApplication(ctx context.Context, root, targetVersion string) error {
	args := fleetComposeArgsAt(root, "up", "-d", "--no-deps", "--force-recreate", "--no-build", "--pull", "never", "fleet-api", "fleet-client")
	if err := RunCompose(ctx, args); err != nil {
		return fmt.Errorf("start HA application: %w", err)
	}
	config, err := loadNodeConfig(filepath.Join(configRoot, "node.env"))
	if err != nil {
		return err
	}
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return err
	}
	for {
		report, err := Status(ctx, filepath.Join(configRoot, "node.env"), true)
		if err == nil {
			publicStatus := probeFleetHost(ctx, tlsConfig, config.VirtualIP, config.NodeIP)
			passiveReady := report.Control != nil && report.Control.FailoverReady
			if (report.Runtime.Role == ha.RoleActive || passiveReady) && applicationReady(report.Runtime, publicStatus, targetVersion) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for updated HA application readiness: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func applicationReady(runtime ha.Status, public fleetHostStatus, targetVersion string) bool {
	publicRoleReady := runtime.Role == ha.RoleActive && public.active || runtime.Role == ha.RolePassive && public.passive
	return runtime.Version == targetVersion &&
		runtime.Observation == ha.ObservationCurrent &&
		publicRoleReady &&
		public.reachable &&
		public.version == targetVersion
}
