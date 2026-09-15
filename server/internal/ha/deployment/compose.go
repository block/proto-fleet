package deployment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"
)

const localDockerHost = "unix:///var/run/docker.sock"

const installedFleetEnvironment = configRoot + "/" + fleetEnvironmentFile

const (
	haTracingHealthTimeout = 15 * time.Second
	haTracingHealthRetry   = 250 * time.Millisecond
)

type fleetApplicationStartDependencies struct {
	environmentPath string
	runCompose      func(context.Context, []string) error
	collectorReady  func(context.Context) error
	warnings        io.Writer
}

func fleetApplicationComposeArgsAtProfile(root, environmentPath string, profile fleetApplicationProfile, operation string, flags ...string) []string {
	return fleetComposeArgsAtProfile(root, environmentPath, profile, operation, slices.Concat(flags, []string{"fleet-api", "fleet-client"}, profile.sidecars())...)
}

func startFleetApplication(ctx context.Context, root string, flags ...string) error {
	return startFleetApplicationWith(ctx, root, fleetApplicationStartDependencies{
		environmentPath: installedFleetEnvironment,
		runCompose:      RunCompose,
		collectorReady:  waitForTracingCollector,
		warnings:        os.Stderr,
	}, flags...)
}

func startFleetApplicationWith(ctx context.Context, root string, deps fleetApplicationStartDependencies, flags ...string) error {
	profile, err := loadFleetApplicationProfileFile(deps.environmentPath, true)
	if err != nil {
		return fmt.Errorf("load application profile: %w", err)
	}
	services := []string{"fleet-api", "fleet-client"}
	if profile.enabled("ENABLE_BETA_ALERTS") {
		services = append(services, "grafana")
	}
	args := fleetComposeArgsAtProfile(root, deps.environmentPath, profile, "up", slices.Concat(flags, []string{"--remove-orphans"}, services)...)
	if err := deps.runCompose(ctx, args); err != nil {
		return err
	}
	if profile.enabled("ENABLE_TRACING") {
		collectorArgs := fleetComposeArgsAtProfile(root, deps.environmentPath, profile, "up", "-d", "--no-deps", "--no-build", "--pull", "never", "otel-collector")
		collectorErr := deps.runCompose(ctx, collectorArgs)
		if collectorErr == nil {
			collectorErr = deps.collectorReady(ctx)
		}
		if collectorErr != nil {
			_, _ = fmt.Fprintf(deps.warnings, "[warning] Fleet is running, but tracing is degraded: %v\n", collectorErr)
		}
	}
	return nil
}

func waitForTracingCollector(ctx context.Context) error {
	healthCtx, cancel := context.WithTimeout(ctx, haTracingHealthTimeout)
	defer cancel()
	client, cleanup := newProbeHTTPClient(nil, nil)
	defer cleanup()
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/", haTracingHealthHostPort)
	if err := waitForHTTPEndpoint(healthCtx, client, endpoint, haTracingHealthRetry); err != nil {
		return fmt.Errorf("collector health endpoint did not become ready: %w", err)
	}
	return nil
}

func waitForHTTPEndpoint(ctx context.Context, client *http.Client, endpoint string, retryInterval time.Duration) error {
	for {
		if endpointReadyWithClient(ctx, client, endpoint) {
			return nil
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for %s: %w", endpoint, ctx.Err())
		case <-timer.C:
		}
	}
}

func fleetApplicationDownArgsAt(root, environmentPath, ownershipMarker string, flags ...string) ([]string, error) {
	return fleetComposeArgsForInstalledProfileAt(root, environmentPath, ownershipMarker, "down", slices.Concat(flags, []string{"--remove-orphans"})...)
}

func fleetSidecarPullArgs(root, environmentPath string, profile fleetApplicationProfile) []string {
	sidecars := profile.sidecars()
	if len(sidecars) == 0 {
		return nil
	}
	return fleetComposeArgsAtProfile(root, environmentPath, profile, "pull", sidecars...)
}

func fleetComposeArgsForInstalledProfileAt(root, environmentPath, ownershipMarker, operation string, flags ...string) ([]string, error) {
	_, markerErr := os.Stat(ownershipMarker)
	if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect installed HA Grafana ownership marker: %w", markerErr)
	}
	profile, err := loadFleetApplicationProfileFile(environmentPath, markerErr == nil)
	if err != nil {
		return nil, err
	}
	return fleetComposeArgsAtProfile(root, environmentPath, profile, operation, flags...), nil
}

// ResetSuperAdminPassword runs the offline fleetd recovery command against the
// installed HA application profile. This preserves the generated environment,
// Compose project, and HA overlay selected by fleet-ha.
func ResetSuperAdminPassword(ctx context.Context, passwordInput io.Reader) error {
	config, err := loadNodeConfig(filepath.Join(configRoot, "node.env"))
	if err != nil {
		return err
	}
	if !config.isDatabaseNode() {
		return errors.New("HA password reset must run on ha-a or ha-b")
	}

	args, err := resetSuperAdminPasswordComposeArgsAt(installRoot, installedFleetEnvironment, haGrafanaVolumeOwnershipMarker)
	if err != nil {
		return err
	}
	return runCompose(ctx, args, passwordInput)
}

func resetSuperAdminPasswordComposeArgsAt(root, environmentPath, ownershipMarker string) ([]string, error) {
	command := []string{"--rm", "--no-deps", "-T", "fleet-api", "/app/fleetd", "admin", "reset-password", "--password-stdin"}
	return fleetComposeArgsForInstalledProfileAt(root, environmentPath, ownershipMarker, "run", command...)
}

// RunCompose isolates Compose from parent variables so only installed HA configuration is interpolated.
func RunCompose(ctx context.Context, args []string) error {
	return runCompose(ctx, args, os.Stdin)
}

func runCompose(ctx context.Context, args []string, stdin io.Reader) error {
	environment, err := composeEnvironment(args)
	if err != nil {
		return err
	}

	commandArgs := append([]string{"--host", localDockerHost, "compose"}, args...)
	command := exec.CommandContext(ctx, "docker", commandArgs...)
	command.Env = environment
	command.Stdin = stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run Docker Compose: %w", err)
	}
	return nil
}

func composeEnvironment(args []string) ([]string, error) {
	// Docker needs PATH to find its Compose plugin. All Compose interpolation
	// inputs come from the explicit, protected env files below.
	environment := []string{"PATH=" + os.Getenv("PATH")}
	for index := 0; index+1 < len(args); index++ {
		if args[index] != "--env-file" || filepath.Base(args[index+1]) != "node.env" {
			continue
		}
		config, err := loadNodeConfig(args[index+1])
		if err != nil {
			return nil, err
		}
		if err := validateNodeConfig(config); err != nil {
			return nil, fmt.Errorf("HA node environment rejected: %w", err)
		}
		// The shared tracing overlay requires DD_HOSTNAME during interpolation.
		// Derive it from the validated HA identity instead of persisting a second hostname.
		return append(environment, "DD_HOSTNAME="+config.NodeName), nil
	}
	return environment, nil
}
