package deployment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/internal/ha"
)

func RequirePassive(ctx context.Context, envPath, targetVersion string) error {
	return ValidatePassiveUpdate(ctx, envPath, targetVersion)
}

func requirePassiveStatus(ctx context.Context, envPath string) (StatusReport, error) {
	report, err := Status(ctx, envPath, true)
	if err != nil {
		return StatusReport{}, err
	}
	if report.Runtime.Observation != ha.ObservationCurrent || report.Runtime.Role != ha.RolePassive {
		return StatusReport{}, fmt.Errorf("HA application update requires a healthy passive node; local role is %s and observation is %s", report.Runtime.Role, report.Runtime.Observation)
	}
	if !rollingUpdateControlReady(report.Control) {
		return StatusReport{}, errors.New("HA application update requires a healthy control path")
	}
	return report, nil
}

// ValidatePassiveUpdate proves that the active peer runs either the local or
// target release before the passive host changes versions.
func ValidatePassiveUpdate(ctx context.Context, envPath, targetVersion string) error {
	report, err := requirePassiveStatus(ctx, envPath)
	if err != nil {
		return err
	}
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return err
	}
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return err
	}
	peerAddress := config.DatabaseAIP
	if config.NodeIP == config.DatabaseAIP {
		peerAddress = config.DatabaseBIP
	}
	peer := probeFleetHost(ctx, tlsConfig, config.VirtualIP, peerAddress)
	if !peer.reachable || !peer.active || (peer.version != report.Runtime.Version && peer.version != targetVersion) {
		return fmt.Errorf("HA application update requires the active peer to run %s or %s", report.Runtime.Version, targetVersion)
	}
	return nil
}

var releaseImageRepositories = [...]string{
	"proto-fleet-api",
	"proto-fleet-client",
}

// PrepareApplicationUpdate builds only the Fleet API and client from a verified release.
func PrepareApplicationUpdate(ctx context.Context, root string) error {
	deps := defaultInstallDependencies()
	if err := validateRelease(ctx, root, deps); err != nil {
		return err
	}
	if _, err := os.Stat(infrastructureCompose); errors.Is(err, os.ErrNotExist) {
		if output, installErr := runCommand(ctx, "install", "-D", "-o", "root", "-g", "root", "-m", "0644", filepath.Join(installRoot, "ha", "compose.yaml"), infrastructureCompose); installErr != nil {
			return fmt.Errorf("pin current HA infrastructure Compose file: %s", commandError(output, installErr))
		}
	} else if err != nil {
		return fmt.Errorf("inspect pinned HA infrastructure Compose file: %w", err)
	}
	if err := pruneReleaseImages(ctx); err != nil {
		return err
	}
	for _, args := range [][]string{{"image", "prune", "-f"}, {"builder", "prune", "-f"}} {
		if output, err := runCommand(ctx, "docker", args...); err != nil {
			return fmt.Errorf("clean previous HA application build artifacts: %s", commandError(output, err))
		}
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

func pruneReleaseImages(ctx context.Context) error {
	for _, repository := range releaseImageRepositories {
		output, err := runCommand(ctx, "docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}", repository)
		if err != nil {
			return fmt.Errorf("list previous HA release images: %s", commandError(output, err))
		}
		for image := range strings.FieldsSeq(string(output)) {
			// Docker refuses to remove images referenced by current or stopped containers.
			_, _ = runCommand(ctx, "docker", "image", "rm", image)
		}
	}
	return nil
}

// StopApplication stops only Fleet containers; the HA substrate keeps running.
func StopApplication(ctx context.Context, root string) error {
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
			if rollingUpdateApplicationReady(report, publicStatus, targetVersion) {
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

func rollingUpdateApplicationReady(report StatusReport, public fleetHostStatus, targetVersion string) bool {
	return report.Runtime.Role == ha.RolePassive &&
		applicationReady(report.Runtime, public, targetVersion) &&
		rollingUpdateControlReady(report.Control)
}

func rollingUpdateControlReady(control *ControlStatus) bool {
	if control == nil || !control.ControlReady {
		return false
	}
	return control.FailoverReady || len(control.ReasonCodes) == 1 && control.ReasonCodes[0] == ReasonFleetVersionMismatch
}

func applicationReady(runtime ha.Status, public fleetHostStatus, targetVersion string) bool {
	publicRoleReady := runtime.Role == ha.RoleActive && public.active || runtime.Role == ha.RolePassive && public.passive
	return runtime.Version == targetVersion &&
		runtime.Observation == ha.ObservationCurrent &&
		publicRoleReady &&
		public.reachable &&
		public.version == targetVersion
}
