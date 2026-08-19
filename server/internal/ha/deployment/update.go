package deployment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"syscall"
	"time"

	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/transportguard"
)

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

func requireUpdatedPeer(ctx context.Context, envPath, targetVersion string) error {
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

func ValidateActiveUpdate(ctx context.Context, envPath, targetVersion string) error {
	_, err := requireActiveStatus(ctx, envPath)
	if err != nil {
		return err
	}
	return requireUpdatedPeer(ctx, envPath, targetVersion)
}

// PrepareApplicationUpdate loads the HA application from a verified release.
func PrepareApplicationUpdate(ctx context.Context, root string) error {
	if err := requireUpdateCompatibleProfile(filepath.Join(configRoot, fleetEnvironmentFile)); err != nil {
		return err
	}
	return prepareApplicationUpdate(ctx, root, defaultInstallDependencies(), RunCompose)
}

func requireUpdateCompatibleProfile(path string) error {
	if err := validateFleetEnvironment(path); err != nil {
		return fmt.Errorf("installed HA profile is incompatible with this release; reinstall both database hosts before updating: %w", err)
	}
	return nil
}

func prepareApplicationUpdate(ctx context.Context, root string, deps installDependencies, runCompose func(context.Context, []string) error) error {
	if err := validateRelease(root, deps.readFile); err != nil {
		return err
	}
	if err := runCompose(ctx, fleetApplicationComposeArgsAt(root, "config", "--quiet")); err != nil {
		return fmt.Errorf("validate HA application update Compose model: %w", err)
	}
	if err := runCompose(ctx, fleetComposeArgsAt(root, "pull", "grafana")); err != nil {
		return fmt.Errorf("stage HA Grafana image: %w", err)
	}
	if output, err := deps.run(ctx, "docker", "load", "--input", filepath.Join(root, "images", "fleet.tar.gz")); err != nil {
		return fmt.Errorf("load HA application update images: %s", commandError(output, err))
	}
	for _, prefix := range []string{"proto-fleet-api:", "proto-fleet-client:"} {
		image, err := composeImage(root, "docker-compose.yaml", prefix, deps.readFile)
		if err != nil {
			return err
		}
		if output, err := deps.run(ctx, "docker", "image", "inspect", image); err != nil {
			return fmt.Errorf("release archive did not load required image %s: %s", image, commandError(output, err))
		}
	}
	return nil
}

// StopApplication rechecks the expected role, then stops the HA application containers.
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
	composeCtx := ctx
	args := fleetApplicationComposeArgsAt(root, "stop")
	if expectedRole == ha.RoleActive {
		// Bound only the serving node's interruption. A passive stop is not on the
		// availability path and can use Compose's normal shutdown behavior.
		stopCtx, cancel := context.WithTimeout(ctx, ha.UpdateActiveStopTimeout)
		defer cancel()
		composeCtx = stopCtx
		args = fleetApplicationComposeArgsAt(root, "stop", "--timeout", "1")
	}
	if err := RunCompose(composeCtx, args); err != nil {
		return fmt.Errorf("stop HA application: %w", err)
	}
	return nil
}

func updatedPassivePeerReady(status fleetHostStatus, targetVersion string) bool {
	return status.reachable && status.passive && status.version == targetVersion
}

// StartApplication starts the target release and proves it serves its observed HA role.
func StartApplication(ctx context.Context, root, targetVersion string, requirePassive, requireFailoverReady bool) error {
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
			requireFailoverReady,
		)
		if readinessErr != nil {
			return readinessErr
		}
		if !ready && !applicationMayConverge(localReport.Runtime, targetVersion, requirePassive) {
			return errors.New("running HA application is not ready; refusing to replace it")
		}
	}
	if statusErr != nil && !errors.Is(statusErr, syscall.ECONNREFUSED) {
		return fmt.Errorf("verify local HA application is stopped: %w", statusErr)
	}
	args := fleetApplicationComposeArgsAt(root, "up", "-d", "--no-deps", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "60")
	if err := RunCompose(ctx, args); err != nil {
		return fmt.Errorf("start HA application: %w", err)
	}
	for {
		report, err := Status(ctx, filepath.Join(configRoot, "node.env"))
		if err == nil {
			publicStatus := probeFleetHost(ctx, tlsConfig, config.VirtualIP, config.NodeIP)
			ready, readinessErr := updatedApplicationReady(report, publicStatus, targetVersion, requirePassive, requireFailoverReady)
			if readinessErr != nil {
				return readinessErr
			}
			if ready {
				_, _ = defaultInstallDependencies().run(ctx, "docker", "image", "prune", "--force")
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
	if runtime.Version != targetVersion {
		return false
	}
	if runtime.Observation != ha.ObservationCurrent {
		return true
	}
	if requirePassive {
		return runtime.Role == ha.RolePassive
	}
	return runtime.Role == ha.RoleActive || runtime.Role == ha.RolePassive
}

func rollingUpdateApplicationReady(report StatusReport, public fleetHostStatus, targetVersion string, requireFailoverReady bool) (bool, error) {
	if report.Runtime.Observation == ha.ObservationCurrent && report.Runtime.Role == ha.RoleActive {
		return false, errors.New("updated node became active; inspect the peer before retrying")
	}
	controlReady := rollingUpdateControlReady(report.Control)
	if requireFailoverReady {
		controlReady = report.Control != nil && report.Control.FailoverReady
	}
	return report.Runtime.Role == ha.RolePassive &&
		applicationReady(report.Runtime, public, targetVersion) &&
		controlReady, nil
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

func updatedApplicationReady(report StatusReport, publicStatus fleetHostStatus, targetVersion string, requirePassive, requireFailoverReady bool) (bool, error) {
	if requirePassive {
		return rollingUpdateApplicationReady(report, publicStatus, targetVersion, requireFailoverReady)
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
	deadline, cancel := context.WithTimeout(ctx, ha.UpdateTakeoverTimeout)
	defer cancel()
	endpoint := "https://" + config.VirtualIP + "/api-proxy/health/active"
	for {
		request, requestErr := http.NewRequestWithContext(deadline, http.MethodGet, endpoint, nil)
		if requestErr != nil {
			return fmt.Errorf("create VIP takeover probe: %w", requestErr)
		}
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_, drainErr := io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			version := response.Header.Get("X-Proto-Fleet-Version")
			if drainErr == nil {
				ready, versionErr := acceptVIPVersion(response.StatusCode, version, targetVersion)
				if versionErr != nil {
					return versionErr
				}
				if ready {
					return nil
				}
			}
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("updated peer did not serve the VIP within %s", ha.UpdateTakeoverTimeout)
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
