package deployment

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/transportguard"
)

// Leaves room for lease expiry and acquisition before falling back to the old release.
const vipTakeoverTimeout = 35 * time.Second

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

func requireActiveStatus(ctx context.Context, envPath string) (StatusReport, error) {
	report, err := Status(ctx, envPath, true)
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

// StopApplication rechecks the expected role, then stops only Fleet containers.
func StopApplication(ctx context.Context, root string, expectedRole ha.Role) error {
	var err error
	switch expectedRole {
	case ha.RolePassive:
		_, err = requirePassiveStatus(ctx, filepath.Join(configRoot, "node.env"))
	case ha.RoleActive:
		_, err = requireActiveStatus(ctx, filepath.Join(configRoot, "node.env"))
	default:
		return fmt.Errorf("unsupported HA role %q", expectedRole)
	}
	if err != nil {
		return fmt.Errorf("refuse to stop HA application after %s role changed: %w", expectedRole, err)
	}
	// Leave two seconds inside the updater's five-second active-stop deadline
	// for Compose and Docker to confirm both containers stopped.
	if err := RunCompose(ctx, fleetComposeArgsAt(root, "stop", "--timeout", "3", "fleet-api", "fleet-client")); err != nil {
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
	// Recovery may replay before Fleet ever stopped. Avoid letting Compose
	// recreate an already-healthy deployment from newly staged image tags.
	ready, err := applicationIsReady(ctx, config, tlsConfig, targetVersion, requirePassive)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	for {
		err := RunCompose(ctx, fleetComposeArgsAt(
			root,
			"up", "-d", "--no-deps", "--no-build", "--pull", "never", "fleet-api", "fleet-client",
		))
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("start HA application after Compose failure %v: %w", err, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
	for {
		ready, err := applicationIsReady(ctx, config, tlsConfig, targetVersion, requirePassive)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for updated HA application readiness: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
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

func applicationIsReady(ctx context.Context, config NodeConfig, tlsConfig *tls.Config, targetVersion string, requirePassive bool) (bool, error) {
	report, err := Status(ctx, filepath.Join(configRoot, "node.env"), true)
	if err != nil {
		return false, nil
	}
	publicStatus := probeFleetHost(ctx, tlsConfig, config.VirtualIP, config.NodeIP)
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
	deadline, cancel := context.WithTimeout(ctx, ha.UpdateTakeoverTimeout)
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
