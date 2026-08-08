package deployment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/transportguard"
)

// Leaves room for lease expiry and acquisition before falling back to the old release.
const vipTakeoverTimeout = 35 * time.Second

func requirePassiveStatus(ctx context.Context, envPath string) (StatusReport, error) {
	report, err := Status(ctx, envPath)
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

func requireActiveStatus(ctx context.Context, envPath string) (StatusReport, error) {
	report, err := Status(ctx, envPath)
	if err != nil {
		return StatusReport{}, err
	}
	if report.Runtime.Observation != ha.ObservationCurrent || report.Runtime.Role != ha.RoleActive || report.Runtime.Endpoint != ha.EndpointHealthy {
		return StatusReport{}, fmt.Errorf(
			"HA completion update requires a healthy active node; local role is %s, observation is %s, and endpoint is %s",
			report.Runtime.Role,
			report.Runtime.Observation,
			report.Runtime.Endpoint,
		)
	}
	if !rollingUpdateControlReady(report.Control) {
		return StatusReport{}, errors.New("HA completion update requires rolling-update readiness")
	}
	return report, nil
}

func RequireUpdatedPeer(ctx context.Context, envPath, targetVersion string) error {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return err
	}
	peerAddress := config.DatabaseAIP
	if config.NodeIP == config.DatabaseAIP {
		peerAddress = config.DatabaseBIP
	}
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return err
	}
	status := probeFleetHost(ctx, tlsConfig, config.VirtualIP, peerAddress)
	if !updatedPassivePeerReady(status, targetVersion) {
		return fmt.Errorf("HA completion update requires the passive peer to run %s", targetVersion)
	}
	return nil
}

func ValidateActiveUpdate(ctx context.Context, envPath, targetVersion string) (string, error) {
	report, err := requireActiveStatus(ctx, envPath)
	if err != nil {
		return "", err
	}
	if err := RequireUpdatedPeer(ctx, envPath, targetVersion); err != nil {
		return "", err
	}
	return report.Runtime.Version, nil
}

// PrepareApplicationUpdate builds only the Fleet API and client from a verified release.
func PrepareApplicationUpdate(ctx context.Context, root string) error {
	deps := defaultInstallDependencies()
	if err := validateRelease(root, deps.readFile); err != nil {
		return err
	}
	if err := validatePinnedInfrastructureGeneration(); err != nil {
		return err
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

func validatePinnedInfrastructureGeneration() error {
	currentVersion, err := deploymentVersion(filepath.Join(installRoot, "version.txt"))
	if err != nil {
		return err
	}
	pinnedImage, err := haDatabaseImage(filepath.Dir(configRoot), os.ReadFile)
	if err != nil {
		return fmt.Errorf("read pinned HA infrastructure version: %w", err)
	}
	return validateInfrastructureGeneration(currentVersion, pinnedImage)
}

func validateInfrastructureGeneration(currentVersion, pinnedImage string) error {
	pinnedVersion := strings.TrimPrefix(pinnedImage, "proto-fleet-timescaledb-ha:")
	pinnedVersion, _, _ = strings.Cut(pinnedVersion, "@")
	if currentVersion != pinnedVersion {
		return fmt.Errorf("chained HA application updates are not supported while infrastructure updates are deferred; application is %s but pinned infrastructure is %s", currentVersion, pinnedVersion)
	}
	return nil
}

func deploymentVersion(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read installed HA application version: %w", err)
	}
	for line := range strings.SplitSeq(string(contents), "\n") {
		if version, ok := strings.CutPrefix(strings.TrimSpace(line), "version:"); ok {
			version = strings.TrimSpace(version)
			if version != "" {
				return version, nil
			}
		}
	}
	return "", errors.New("installed HA application version is missing")
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

// StopApplication rechecks the expected role, then stops only Fleet containers.
func StopApplication(ctx context.Context, root string, expectedRole ha.RuntimeRole) error {
	var err error
	switch expectedRole {
	case ha.RolePassive:
		_, err = requirePassiveStatus(ctx, filepath.Join(configRoot, "node.env"))
	case ha.RoleActive:
		_, err = requireActiveStatus(ctx, filepath.Join(configRoot, "node.env"))
	case ha.RoleInitializing, ha.RoleDegraded:
		return fmt.Errorf("unsupported HA role %q", expectedRole)
	}
	if err != nil {
		return fmt.Errorf("refuse to stop HA application after %s role changed: %w", expectedRole, err)
	}
	// The crash-only design intentionally has no maintenance lease. If the role
	// changes after this final proof, normal update recovery restarts Fleet.
	if err := RunCompose(ctx, fleetComposeArgsAt(root, "stop", "fleet-api", "fleet-client")); err != nil {
		return fmt.Errorf("stop HA application: %w", err)
	}
	return nil
}

func updatedPassivePeerReady(status fleetHostStatus, targetVersion string) bool {
	return status.reachable && status.passive && status.version == targetVersion
}

// StartApplication starts the target release and proves it serves its observed HA role.
func StartApplication(ctx context.Context, root, targetVersion string, requirePassive bool) error {
	return startApplication(ctx, root, targetVersion, requirePassive)
}

func startApplication(ctx context.Context, root, targetVersion string, requirePassive bool) error {
	config, err := loadNodeConfig(filepath.Join(configRoot, "node.env"))
	if err != nil {
		return err
	}
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return err
	}
	localReport, statusErr := Status(ctx, filepath.Join(configRoot, "node.env"))
	if statusErr == nil {
		ready, readinessErr := updatedApplicationReady(
			localReport,
			probeFleetHost(ctx, tlsConfig, config.VirtualIP, config.NodeIP),
			targetVersion,
			requirePassive,
		)
		if readinessErr != nil {
			return readinessErr
		}
		if ready {
			return nil
		}
		if !applicationMayConverge(localReport.Runtime, targetVersion, requirePassive) {
			return errors.New("running HA application is not ready; refusing to replace it")
		}
	}
	if statusErr != nil && !errors.Is(statusErr, syscall.ECONNREFUSED) {
		return fmt.Errorf("verify local HA application is stopped: %w", statusErr)
	}
	args := fleetComposeArgsAt(root, "up", "-d", "--no-deps", "--no-build", "--pull", "never", "fleet-api", "fleet-client")
	if err := RunCompose(ctx, args); err != nil {
		return fmt.Errorf("start HA application: %w", err)
	}
	for {
		report, err := Status(ctx, filepath.Join(configRoot, "node.env"))
		if err == nil {
			publicStatus := probeFleetHost(ctx, tlsConfig, config.VirtualIP, config.NodeIP)
			ready, readinessErr := updatedApplicationReady(report, publicStatus, targetVersion, requirePassive)
			if readinessErr != nil {
				return readinessErr
			}
			if ready {
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

func applicationMayConverge(runtime ha.Status, targetVersion string, requirePassive bool) bool {
	if runtime.Version != targetVersion || runtime.Observation != ha.ObservationCurrent {
		return false
	}
	if requirePassive {
		return runtime.Role == ha.RolePassive
	}
	return runtime.Role == ha.RoleActive || runtime.Role == ha.RolePassive
}

func rollingUpdateApplicationReady(report StatusReport, public fleetHostStatus, targetVersion string) (bool, error) {
	if report.Runtime.Observation == ha.ObservationCurrent && report.Runtime.Role == ha.RoleActive {
		return false, errors.New("updated node became active; inspect the peer before retrying")
	}
	return report.Runtime.Role == ha.RolePassive &&
		applicationReady(report.Runtime, public, targetVersion) &&
		rollingUpdateControlReady(report.Control), nil
}

func rollingUpdateControlReady(control *ControlStatus) bool {
	if control == nil || !control.ControlReady {
		return false
	}
	return control.FailoverReady || ExpectedRollingVersionMismatch(control)
}

// ExpectedRollingVersionMismatch is the only degraded state allowed during a rolling update.
func ExpectedRollingVersionMismatch(control *ControlStatus) bool {
	return control != nil && control.ControlReady &&
		len(control.ReasonCodes) == 1 && control.ReasonCodes[0] == ReasonFleetVersionMismatch
}

func updatedApplicationReady(report StatusReport, publicStatus fleetHostStatus, targetVersion string, requirePassive bool) (bool, error) {
	if requirePassive {
		return rollingUpdateApplicationReady(report, publicStatus, targetVersion)
	}
	if report.Control == nil || !report.Control.ControlReady {
		return false, nil
	}
	return applicationReady(report.Runtime, publicStatus, targetVersion), nil
}

func applicationReady(runtime ha.Status, public fleetHostStatus, targetVersion string) bool {
	publicRoleReady := runtime.Role == ha.RoleActive && public.active || runtime.Role == ha.RolePassive && public.passive
	return runtime.Version == targetVersion &&
		runtime.Observation == ha.ObservationCurrent &&
		publicRoleReady &&
		public.reachable &&
		public.version == targetVersion
}

func WaitForVIPVersion(ctx context.Context, envPath, targetVersion string) error {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return err
	}
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return err
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig, Proxy: nil}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: transportguard.RejectRedirect}
	defer transport.CloseIdleConnections()
	deadline, cancel := context.WithTimeout(ctx, vipTakeoverTimeout)
	defer cancel()
	endpoint := "https://" + config.VirtualIP + "/api-proxy/health"
	for {
		request, requestErr := http.NewRequestWithContext(deadline, http.MethodGet, endpoint, nil)
		if requestErr != nil {
			return fmt.Errorf("create VIP takeover probe: %w", requestErr)
		}
		response, requestErr := client.Do(request)
		if requestErr == nil {
			response.Body.Close()
			version := response.Header.Get("X-Proto-Fleet-Version")
			ready, versionErr := acceptVIPVersion(response.StatusCode, version, targetVersion)
			if versionErr != nil {
				return versionErr
			}
			if ready {
				return nil
			}
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("updated peer did not serve the VIP within %s", vipTakeoverTimeout)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func acceptVIPVersion(status int, version, targetVersion string) (bool, error) {
	if status != http.StatusOK {
		return false, nil
	}
	if version == targetVersion {
		return true, nil
	}
	if version != "" {
		return false, fmt.Errorf("VIP is served by %s, expected %s", version, targetVersion)
	}
	return false, nil
}
